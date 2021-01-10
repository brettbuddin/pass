package passutil

import (
	"fmt"
	"net/http"

	"github.com/brettbuddin/pass"
)

var _ pass.RouteRegistrar = (*ServeMux)(nil)

// NewServeMux creates a ServeMux.
func NewServeMux(mux *http.ServeMux) *ServeMux {
	return &ServeMux{
		ServeMux:         mux,
		MethodNotAllowed: http.HandlerFunc(defaultMethodNotAllowed),
	}
}

// ServeMux is a wrapper for net/http.ServeMux that makes it a
// pass.RouteRegistrar.
type ServeMux struct {
	// ServeMux to wrap.
	*http.ServeMux

	// Handler to use when responding to "405 Method Not Allowed" errors. You
	// should probably override this.
	MethodNotAllowed http.Handler
}

// Method implements pass.RouteRegistrar.
func (m *ServeMux) Method(method, path string, handler http.Handler) {
	notAllowed := m.MethodNotAllowed
	m.ServeMux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == method {
			handler.ServeHTTP(w, r)
			return
		}
		notAllowed.ServeHTTP(w, r)
	})
}

func defaultMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "text/plain")
	w.WriteHeader(http.StatusMethodNotAllowed)
	fmt.Fprintf(w, "method not allowed: %s\n", r.Method)
}
