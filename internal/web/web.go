// Package web owns loopback listening and the public HTTP boundary.
package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/markdlabrecque/composure/internal/content"
	"github.com/markdlabrecque/composure/internal/render"
)

// ResolveLoopback validates every DNS result and returns an exact bind address.
func ResolveLoopback(ctx context.Context, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid address %q", address)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return "", fmt.Errorf("invalid port %q", port)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("cannot resolve address %q", address)
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() || ip.Zone != "" {
			return "", fmt.Errorf("address %s is not loopback; phase 1 serves only 127.0.0.1, ::1 or localhost", address)
		}
	}
	return net.JoinHostPort(ips[0].IP.String(), port), nil
}

func Handler(repository content.Repository, port string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := repository.PublishedByPath(r.Context(), r.URL.Path)
		if errors.Is(err, content.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "cannot load page", 500)
			return
		}
		html, err := render.Page(snapshot)
		if err != nil {
			http.Error(w, "cannot render page", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	})
	protected := http.NewCrossOriginProtection().Handler(mux)
	allowed := map[string]bool{net.JoinHostPort("127.0.0.1", port): true, net.JoinHostPort("localhost", port): true, net.JoinHostPort("::1", port): true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "unexpected Host", http.StatusBadRequest)
			return
		}
		// Reject raw aliases before ServeMux can clean or redirect them.
		if strings.Contains(r.URL.EscapedPath(), "%") || path.Clean(r.URL.Path) != r.URL.Path {
			http.NotFound(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

func Server(repository content.Repository, listener net.Listener) *http.Server {
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return &http.Server{Handler: Handler(repository, port), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
}
