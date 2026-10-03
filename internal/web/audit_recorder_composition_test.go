package web

import (
	"context"
	"net"
	"testing"

	"github.com/markdlabrecque/composure/internal/audit"
)

type auditRecorderProvider interface {
	auditRecorder() audit.Recorder
}

type recorderCompositionListener struct{}

func (recorderCompositionListener) Accept() (net.Conn, error) { return nil, context.Canceled }
func (recorderCompositionListener) Close() error              { return nil }
func (recorderCompositionListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

func TestServerRetainsInjectedAuditRecorderInHandlerComposition(t *testing.T) {
	recorder := audit.NewLogRecorder(nil)
	server := Server(nil, recorder, recorderCompositionListener{})
	provider, ok := server.Handler.(auditRecorderProvider)
	if !ok {
		t.Fatal("web server handler does not retain its injected audit recorder")
	}
	if got := provider.auditRecorder(); got != recorder {
		t.Fatal("web server handler retained a different audit recorder")
	}
}
