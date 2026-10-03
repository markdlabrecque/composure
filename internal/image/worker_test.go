package image

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/markdlabrecque/composure/internal/upload"
)

const workerTestTimeout = 2 * time.Second

func TestWorkerProcessesConcurrentSubmissionsSequentially(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	var calls int

	worker := newWorker(1, func(ctx context.Context, filename string, encoded []byte, fieldSizeLimit *int64, fieldDimensionLimit *upload.DimensionLimits) ([]byte, upload.Kind, error) {
		calls++
		switch calls {
		case 1:
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return nil, "", ctx.Err()
			}
		case 2:
			close(secondStarted)
		}
		return bytes.Clone(encoded), upload.PNG, nil
	})
	t.Cleanup(worker.Shutdown)

	firstDone := submitAsync(worker, context.Background(), "first.png", []byte("first"))
	waitClosed(t, firstStarted, "first job to start")
	secondDone := submitAsync(worker, context.Background(), "second.png", []byte("second"))

	select {
	case <-secondStarted:
		t.Fatal("second job started while first job was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	waitClosed(t, secondStarted, "second job to start after first completed")

	for i, result := range []workerResult{waitResult(t, firstDone), waitResult(t, secondDone)} {
		if result.err != nil {
			t.Fatalf("job %d returned error: %v", i+1, result.err)
		}
	}
}

func TestWorkerReturnsFailureAndContinuesWithLaterJobs(t *testing.T) {
	wantErr := errors.New("re-encode failed")
	var calls int
	worker := newWorker(1, func(context.Context, string, []byte, *int64, *upload.DimensionLimits) ([]byte, upload.Kind, error) {
		calls++
		if calls == 1 {
			return nil, "", wantErr
		}
		return []byte("stored"), upload.PNG, nil
	})
	t.Cleanup(worker.Shutdown)

	if _, _, err := worker.Reencode(context.Background(), "bad.png", []byte("bad"), nil, nil); !errors.Is(err, wantErr) {
		t.Fatalf("first job error = %v, want %v", err, wantErr)
	}
	stored, kind, err := worker.Reencode(context.Background(), "good.png", []byte("good"), nil, nil)
	if err != nil {
		t.Fatalf("later job stalled or failed after earlier error: %v", err)
	}
	if string(stored) != "stored" || kind != upload.PNG {
		t.Fatalf("later job result = %q, %q; want stored PNG", stored, kind)
	}
}

func TestWorkersForIndependentSitesRunIndependently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	fn := func(ctx context.Context, filename string, encoded []byte, fieldSizeLimit *int64, fieldDimensionLimit *upload.DimensionLimits) ([]byte, upload.Kind, error) {
		started <- filename
		select {
		case <-release:
			return encoded, upload.PNG, nil
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	firstSite := newWorker(1, fn)
	secondSite := newWorker(1, fn)
	t.Cleanup(firstSite.Shutdown)
	t.Cleanup(secondSite.Shutdown)

	firstDone := submitAsync(firstSite, context.Background(), "first-site.png", nil)
	secondDone := submitAsync(secondSite, context.Background(), "second-site.png", nil)
	waitReceive(t, started, "first site job to start")
	waitReceive(t, started, "second site job to start concurrently")
	close(release)
	if result := waitResult(t, firstDone); result.err != nil {
		t.Fatalf("first site job failed: %v", result.err)
	}
	if result := waitResult(t, secondDone); result.err != nil {
		t.Fatalf("second site job failed: %v", result.err)
	}
}

func TestWorkerQueueIsBoundedAndBlockedSubmissionCanBeCanceled(t *testing.T) {
	started := make(chan string, 1)
	release := make(chan struct{})
	worker := newWorker(0, func(ctx context.Context, filename string, encoded []byte, fieldSizeLimit *int64, fieldDimensionLimit *upload.DimensionLimits) ([]byte, upload.Kind, error) {
		started <- filename
		select {
		case <-release:
			return encoded, upload.PNG, nil
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	})
	t.Cleanup(worker.Shutdown)

	firstDone := submitAsync(worker, context.Background(), "running.png", nil)
	waitReceive(t, started, "running job to start")

	ctx, cancel := context.WithCancel(context.Background())
	blockedDone := submitAsync(worker, ctx, "canceled.png", nil)
	cancel()
	if result := waitResult(t, blockedDone); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("blocked submission error = %v, want context canceled", result.err)
	}

	close(release)
	if result := waitResult(t, firstDone); result.err != nil {
		t.Fatalf("running job failed: %v", result.err)
	}
	select {
	case filename := <-started:
		if filename == "canceled.png" {
			t.Fatal("canceled submission was processed")
		}
	default:
	}
}

func TestWorkerShutdownCancelsJobsAndWaitsForWorkerToExit(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	allowExit := make(chan struct{})
	worker := newWorker(1, func(ctx context.Context, filename string, encoded []byte, fieldSizeLimit *int64, fieldDimensionLimit *upload.DimensionLimits) ([]byte, upload.Kind, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-allowExit
		return nil, "", ctx.Err()
	})

	runningDone := submitAsync(worker, context.Background(), "running.png", nil)
	waitClosed(t, started, "running job to start")
	queuedDone := submitAsync(worker, context.Background(), "queued.png", nil)
	shutdownDone := make(chan struct{})
	go func() {
		worker.Shutdown()
		close(shutdownDone)
	}()

	waitClosed(t, canceled, "running job context to be canceled")
	select {
	case <-shutdownDone:
		t.Fatal("Shutdown returned before the active worker function exited")
	case <-time.After(50 * time.Millisecond):
	}
	close(allowExit)
	waitClosed(t, shutdownDone, "Shutdown to join the worker goroutine")

	if result := waitResult(t, runningDone); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("running job error = %v, want context canceled", result.err)
	}
	if result := waitResult(t, queuedDone); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("queued job error = %v, want context canceled", result.err)
	}
}

func TestNewWorkerUsesReencode(t *testing.T) {
	worker := NewWorker(1)
	t.Cleanup(worker.Shutdown)
	input := readFixture(t, "metadata.png")

	stored, kind, err := worker.Reencode(context.Background(), "image.png", input, nil, nil)
	if err != nil {
		t.Fatalf("default worker re-encode failed: %v", err)
	}
	if kind != upload.PNG {
		t.Fatalf("stored kind = %q, want %q", kind, upload.PNG)
	}
	if bytes.Equal(stored, input) {
		t.Fatal("default worker returned uploaded bytes without re-encoding them")
	}
	if containsEXIF(stored) || containsGPSDirectory(stored) {
		t.Fatal("default worker did not strip image metadata")
	}
}

type workerResult struct {
	encoded []byte
	kind    upload.Kind
	err     error
}

func submitAsync(worker *Worker, ctx context.Context, filename string, encoded []byte) <-chan workerResult {
	done := make(chan workerResult, 1)
	go func() {
		stored, kind, err := worker.Reencode(ctx, filename, encoded, nil, nil)
		done <- workerResult{encoded: stored, kind: kind, err: err}
	}()
	return done
}

func waitResult(t *testing.T, done <-chan workerResult) workerResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(workerTestTimeout):
		t.Fatal("timed out waiting for worker result")
		return workerResult{}
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(workerTestTimeout):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitReceive(t *testing.T, ch <-chan string, description string) string {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(workerTestTimeout):
		t.Fatalf("timed out waiting for %s", description)
		return ""
	}
}
