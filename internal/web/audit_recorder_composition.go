package web

import (
	"net/http"

	"github.com/markdlabrecque/composure/internal/audit"
)

type auditRecorderHandler struct {
	http.Handler
	recorder audit.Recorder
}

func (h *auditRecorderHandler) auditRecorder() audit.Recorder {
	return h.recorder
}
