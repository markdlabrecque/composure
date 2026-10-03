package image

import (
	"context"
	"sync"

	"github.com/markdlabrecque/composure/internal/upload"
)

type reencodeFunc func(context.Context, string, []byte, *int64, *upload.DimensionLimits) ([]byte, upload.Kind, error)

type jobResult struct {
	encoded []byte
	kind    upload.Kind
	err     error
}

type workerJob struct {
	ctx          context.Context
	cancel       context.CancelFunc
	stopShutdown func() bool
	stopCaller   func() bool
	filename     string
	encoded      []byte
	sizeLimit    *int64
	dimension    *upload.DimensionLimits
	done         chan jobResult
}

// Worker serializes image re-encoding jobs for one site.
type Worker struct {
	capacity int
	reencode reencodeFunc

	mu      sync.Mutex
	changed chan struct{}
	queue   []*workerJob
	active  *workerJob
	closed  bool

	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	shutdown sync.Once
}

// NewWorker creates a per-site image worker using Reencode for each job.
func NewWorker(queueCapacity int) *Worker {
	return newWorker(queueCapacity, func(_ context.Context, filename string, encoded []byte, fieldSizeLimit *int64, fieldDimensionLimit *upload.DimensionLimits) ([]byte, upload.Kind, error) {
		return Reencode(filename, encoded, fieldSizeLimit, fieldDimensionLimit)
	})
}

func newWorker(queueCapacity int, process reencodeFunc) *Worker {
	if queueCapacity < 0 {
		queueCapacity = 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{
		capacity: queueCapacity,
		reencode: process,
		changed:  make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

// Reencode submits an image for serialized processing and waits for its result.
// A caller may cancel while waiting for queue space or while its job is queued.
func (w *Worker) Reencode(ctx context.Context, filename string, encoded []byte, fieldSizeLimit *int64, fieldDimensionLimit *upload.DimensionLimits) ([]byte, upload.Kind, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	jobCtx, cancel := context.WithCancel(ctx)
	job := &workerJob{
		ctx:       jobCtx,
		cancel:    cancel,
		filename:  filename,
		encoded:   encoded,
		sizeLimit: fieldSizeLimit,
		dimension: fieldDimensionLimit,
		done:      make(chan jobResult, 1),
	}

	for {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			cancel()
			return nil, "", context.Canceled
		}
		if err := ctx.Err(); err != nil {
			w.mu.Unlock()
			cancel()
			return nil, "", err
		}
		if len(w.queue) < w.capacity || (w.active == nil && len(w.queue) == 0) {
			job.stopShutdown = context.AfterFunc(w.ctx, cancel)
			w.queue = append(w.queue, job)
			job.stopCaller = context.AfterFunc(ctx, func() { w.cancelQueued(job, ctx.Err()) })
			w.signalLocked()
			w.mu.Unlock()
			break
		}
		changed := w.changed
		w.mu.Unlock()

		select {
		case <-ctx.Done():
			cancel()
			return nil, "", ctx.Err()
		case <-w.ctx.Done():
			cancel()
			return nil, "", context.Canceled
		case <-changed:
		}
	}

	select {
	case result := <-job.done:
		return result.encoded, result.kind, result.err
	case <-ctx.Done():
		select {
		case result := <-job.done:
			return result.encoded, result.kind, result.err
		default:
			return nil, "", ctx.Err()
		}
	case <-w.ctx.Done():
		select {
		case result := <-job.done:
			return result.encoded, result.kind, result.err
		default:
			return nil, "", context.Canceled
		}
	}
}

// Shutdown cancels queued and active jobs, then waits for the consumer to exit.
func (w *Worker) Shutdown() {
	w.shutdown.Do(func() {
		w.mu.Lock()
		w.closed = true
		w.cancel()
		for _, job := range w.queue {
			job.cancel()
			if job.stopShutdown != nil {
				job.stopShutdown()
			}
			if job.stopCaller != nil {
				job.stopCaller()
			}
			completeJob(job, jobResult{err: context.Canceled})
		}
		clear(w.queue)
		w.queue = nil
		w.signalLocked()
		w.mu.Unlock()
	})
	<-w.done
}

func (w *Worker) run() {
	defer close(w.done)
	for {
		w.mu.Lock()
		for !w.closed && len(w.queue) == 0 {
			changed := w.changed
			w.mu.Unlock()
			<-changed
			w.mu.Lock()
		}
		if w.closed {
			w.mu.Unlock()
			return
		}
		job := w.queue[0]
		w.queue[0] = nil
		w.queue = w.queue[1:]
		w.active = job
		w.signalLocked()
		w.mu.Unlock()

		encoded, kind, err := w.reencode(job.ctx, job.filename, job.encoded, job.sizeLimit, job.dimension)
		if job.ctx.Err() != nil {
			encoded, kind, err = nil, "", job.ctx.Err()
		}
		job.cancel()
		if job.stopShutdown != nil {
			job.stopShutdown()
		}
		if job.stopCaller != nil {
			job.stopCaller()
		}

		w.mu.Lock()
		w.active = nil
		w.signalLocked()
		w.mu.Unlock()

		job.encoded = nil
		completeJob(job, jobResult{encoded: encoded, kind: kind, err: err})
	}
}

func (w *Worker) cancelQueued(job *workerJob, err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, queued := range w.queue {
		if queued == job {
			copy(w.queue[i:], w.queue[i+1:])
			w.queue[len(w.queue)-1] = nil
			w.queue = w.queue[:len(w.queue)-1]
			job.encoded = nil
			job.cancel()
			if job.stopShutdown != nil {
				job.stopShutdown()
			}
			if job.stopCaller != nil {
				job.stopCaller()
			}
			completeJob(job, jobResult{err: err})
			w.signalLocked()
			return
		}
	}
}

func (w *Worker) signalLocked() {
	close(w.changed)
	w.changed = make(chan struct{})
}

func completeJob(job *workerJob, result jobResult) {
	job.filename = ""
	job.encoded = nil
	job.sizeLimit = nil
	job.dimension = nil
	job.done <- result
}
