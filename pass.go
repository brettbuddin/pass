// Package pass provides a small and configurable reverse-proxy.
package pass

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"time"

	"github.com/go-chi/chi"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
)

// Manifest is a list of upstream services in which to proxy.
type Manifest struct {
	Annotations map[string]string `hcl:"annotations,optional"` // Annotations to be used by other libraries
	Upstreams   []Upstream        `hcl:"upstream,block"`       // Upstream route configurations
	PrefixPath  string            `hcl:"prefix_path,optional"` // Prefix to add to all upstream routes. Stripped when proxying.
}

// Upstream is an upstream service in which to proxy.
type Upstream struct {
	Identifier      string            `hcl:",label"`                     // Human identifier for the upstream
	Annotations     map[string]string `hcl:"annotations,optional"`       // Annotations to be used by other libraries
	Destination     string            `hcl:"destination"`                // Scheme and Hostname of the upstream component
	Routes          []Route           `hcl:"route,block"`                // Routes to accept
	FlushIntervalMS int               `hcl:"flush_interval_ms,optional"` // httputil.ReverseProxy.FlushInterval value in milliseconds
	Owner           string            `hcl:"owner,optional"`             // Team that owns the upstream component
	PrefixPath      string            `hcl:"prefix_path,optional"`       // Prefix to add to all routes. Stripped when proxying.
}

// Route is an individual HTTP method/path combination in which to proxy.
type Route struct {
	Methods []string `hcl:"methods"` // HTTP Methods
	Path    string   `hcl:"path"`    // HTTP Path
}

// LoadManifest parses an HCL file containing the manifest.
func LoadManifest(filename string, ectx *hcl.EvalContext) (*Manifest, error) {
	var m Manifest
	err := hclsimple.DecodeFile(filename, ectx, &m)
	if err != nil {
		return nil, err
	}

	// Establish defaults for annotation maps so the caller can simply ask about
	// keys without caring about nil values.
	if m.Annotations == nil {
		m.Annotations = map[string]string{}
	}
	for i := range m.Upstreams {
		if m.Upstreams[i].Annotations == nil {
			m.Upstreams[i].Annotations = map[string]string{}
		}
	}
	return &m, nil
}

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

	proxy := &Proxy{
		manifest: m,
		router:   chi.NewRouter(),
		root:     path.Join(cfg.root, m.PrefixPath),
	}
	if cfg.notFoundHandler != nil {
		proxy.router.NotFound(cfg.notFoundHandler)
	}

	for _, u := range m.Upstreams {
		rproxy, err := newReverseProxy(u, cfg)
		if err != nil {
			return nil, err
		}

		for _, rt := range u.Routes {
			// Construct the full prefix for mounting. All of this will be
			// stripped from the request we pass upstream.
			prefix := path.Join(proxy.root, u.PrefixPath)

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
				proxy.router.Method(
					method,
					path.Join(prefix, rt.Path),
					http.StripPrefix(prefix, proxyHandler(rproxy, observe)),
				)
			}
		}
	}
	return proxy, nil
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

// Middleware is an http.Handler middleware.
type Middleware func(http.Handler) http.Handler

// MountOption is a functional option used when mounting a manifest to a router.
type MountOption func(*mountConfig)

// ObserveFunction is a function called just before a request is proxied to an
// upstream host. It provides an opportunity to perform logging and update
// metrics with information about the route.
type ObserveFunction func(*http.Request, *RouteInfo)

// RouteInfo is a structure that communicates route information to an
// ObserveFunction.
type RouteInfo struct {
	RouteMethod        string
	RoutePath          string
	RoutePrefix        string
	UpstreamHost       string
	UpstreamIdentifier string
	UpstreamOwner      string
}

// WithObserveFunction sets an ObserveFunction to use for all requests being
// proxied upstream.
func WithObserveFunction(fn ObserveFunction) MountOption {
	return func(c *mountConfig) {
		c.observe = fn
	}
}

// WithRoot informs the proxy of the root mount point. This root prefix will be
// stripped away from all requests sent upstream.
func WithRoot(prefix string) MountOption {
	return func(c *mountConfig) {
		c.root = prefix
	}
}

// WithErrorLog specifies an error logger to use when reporting upstream
// communication errors instead of the log package's default logger.
func WithErrorLog(l *log.Logger) MountOption {
	return func(c *mountConfig) {
		c.errorLog = l
	}
}

// BufferPool is an alias for httputil.BufferPool
type BufferPool = httputil.BufferPool

// WithBufferPool specifies a BufferPool to obtain byte slices for io.CopyBuffer
// operations when copying responses.
func WithBufferPool(p BufferPool) MountOption {
	return func(c *mountConfig) {
		c.bufferPool = p
	}
}

// ResponseModifier is a function that modifies upstream responses before as
// they are returned to the client.
type ResponseModifier func(*http.Response) error

// WithResponseModifier specifies a ResponseModifier function to apply to
// responses coming from upstream service. If the upstream is unreachable, this
// function will not be called.
func WithResponseModifier(fn ResponseModifier) MountOption {
	return func(c *mountConfig) {
		c.responseModifier = fn
	}
}

// ErrorHandler is a function that handles errors on behalf of the proxy. Errors
// returned from ResponseModifier functions will also be handled by this
// function.
type ErrorHandler func(http.ResponseWriter, *http.Request, error)

// WithErrorHandler specifies an ErrorHandler to call in the face on any errors
// communicating with the upstream service.
func WithErrorHandler(fn ErrorHandler) MountOption {
	return func(c *mountConfig) {
		c.errorHandler = fn
	}
}

// RequestModifier is a function that modifies a request
type RequestModifier func(*http.Request)

// WithRequestModifier specifies a RequestModifer to apply to all outgoing
// requests.
func WithRequestModifier(fn RequestModifier) MountOption {
	return func(c *mountConfig) {
		c.requestModifier = fn
	}
}

// WithTransport specifies an http.RoundTripper to use instead of
// http.DefaultTransport.
func WithTransport(t http.RoundTripper) MountOption {
	return func(c *mountConfig) {
		c.transport = t
	}
}

// WithNotFound specifies an http.HandlerFunc to use if no routes in the
// manifest match. Use this for fall-through behavior to delegate to existing
// (in-process) routes.
func WithNotFound(h http.HandlerFunc) MountOption {
	return func(c *mountConfig) {
		c.notFoundHandler = h
	}
}

// mountConfig contains realized configuration for mounting routes.
type mountConfig struct {
	// Pass configuration
	observe ObserveFunction
	root    string

	// httputil.ReverseProxy configuration
	bufferPool       httputil.BufferPool
	errorHandler     ErrorHandler
	errorLog         *log.Logger
	requestModifier  RequestModifier
	responseModifier ResponseModifier
	transport        http.RoundTripper
	notFoundHandler  http.HandlerFunc
}

// newMountConfig creates a mountConfig with established defaults.
func newMountConfig() mountConfig {
	return mountConfig{
		errorLog: log.New(io.Discard, "", log.LstdFlags),
	}
}
