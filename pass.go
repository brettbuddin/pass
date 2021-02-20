// Package pass provides a small and configurable reverse-proxy.
package pass

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"time"

	"github.com/go-chi/chi"
)

var ErrMissingUpstreamForMiddleware = fmt.Errorf("upstream missing for middleware stack")

// Proxy is a reverse-proxy.
type Proxy struct {
	manifest *Manifest
	router   chi.Router
	root     string
}

// New creates a new Proxy with the Manifest's routes mounted to it.
func New(m *Manifest, opts ...MountOption) (*Proxy, error) {
	cfg := newMountConfig()
	for _, o := range opts {
		o(&cfg)
	}

	// Verify that the middleware stacks reference real upstreams
	for k := range cfg.middleware {
		if _, ok := m.upstreamIndex[k]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrMissingUpstreamForMiddleware, k)
		}
	}

	proxy := &Proxy{
		manifest: m,
		router:   chi.NewRouter(),
		root:     path.Join(cfg.root, m.PrefixPath),
	}
	if cfg.notFoundHandler != nil {
		proxy.router.NotFound(cfg.notFoundHandler)
	}

	for _, u := range m.Upstreams {
		var err error
		proxy.router.Group(func(r chi.Router) {
			if mstack, ok := cfg.middleware[u.Identifier]; ok {
				r.Use(mstack...)
			}
			err = mount(r, proxy.root, u, cfg)
		})
		if err != nil {
			return nil, err
		}
	}
	return proxy, nil
}

func mount(router chi.Router, rootPath string, u Upstream, cfg mountConfig) error {
	rproxy, err := newReverseProxy(u, cfg)
	if err != nil {
		fmt.Println(err)
		return err
	}

	for _, rt := range u.Routes {
		// Construct the full prefix for mounting. All of this will be
		// stripped from the request we pass upstream.
		prefix := path.Join(rootPath, u.PrefixPath)

		for _, method := range rt.Methods {
			// Defer creation of the RouteInfo structure to request-time.
			observe := func(r *http.Request) {
				if cfg.observe == nil {
					return
				}
				cfg.observe(r, &RouteInfo{
					RouteMethod:        method,
					RoutePath:          rt.Path,
					RoutePrefix:        prefix,
					UpstreamHost:       u.Destination,
					UpstreamIdentifier: u.Identifier,
					UpstreamOwner:      u.Owner,
				})
			}

			path := path.Join(prefix, rt.Path)
			handler := http.StripPrefix(prefix, proxyHandler(rproxy, observe))
			router.Method(method, path, handler)
		}
	}

	return nil
}

// Root returns the root specified at Proxy creation + the "prefix_path"
// specified in the Manifest.
func (p *Proxy) Root() string {
	if p.root == "" {
		return "/"
	}
	return p.root
}

// Upstreams returns the Upstream services registered with this Proxy.
func (p *Proxy) Upstreams() []Upstream {
	return p.manifest.Upstreams
}

// ServeHTTP implements net/http.Handler
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

// newReverseProxy creates and configures a new httputil.ReverseProxy.
func newReverseProxy(u Upstream, cfg mountConfig) (*httputil.ReverseProxy, error) {
	dest, err := url.Parse(u.Destination)
	if err != nil {
		return nil, err
	}

	if dest.Scheme == "" {
		return nil, fmt.Errorf("missing scheme: %q", u.Destination)
	}

	proxy := httputil.NewSingleHostReverseProxy(dest)
	setDirector(proxy, dest.Host, cfg.requestModifier)
	if cfg.transport != nil {
		proxy.Transport = cfg.transport
	}
	if cfg.errorLog != nil {
		proxy.ErrorLog = cfg.errorLog
	}
	if cfg.bufferPool != nil {
		proxy.BufferPool = cfg.bufferPool
	}
	if cfg.responseModifier != nil {
		proxy.ModifyResponse = cfg.responseModifier
	}
	if cfg.errorHandler != nil {
		proxy.ErrorHandler = cfg.errorHandler
	}
	proxy.FlushInterval = time.Duration(u.FlushIntervalMS) * time.Millisecond

	return proxy, nil
}

// proxyHandler is an HTTP that hands requests off to a httputil.ReverseProxy.
// It performs some request-level logging.
func proxyHandler(proxy *httputil.ReverseProxy, observe func(r *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe(r)
		proxy.ServeHTTP(w, r)
	})
}

// setDirector replaces the existing proxy's director function with one of our
// own to smooth over some behavior. It also applies any request modification
// configured by the caller.
func setDirector(p *httputil.ReverseProxy, destHost string, modifier RequestModifier) {
	base := p.Director
	p.Director = func(r *http.Request) {
		base(r)

		// Override r.Host to prevent us sending this request back to ourselves.
		// A bug in the stdlib causes this value to be preferred over the
		// r.URL.Host (which is set in the default Director) if r.Host isn't
		// empty (which it isn't).
		// https://github.com/golang/go/issues/28168
		r.Host = destHost

		if modifier != nil {
			modifier(r)
		}
	}
}
