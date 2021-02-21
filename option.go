package pass

import (
	"io"
	"log"
	"net/http"
	"net/http/httputil"
)

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

// WithUpstreamMiddleware registers a middleware stack for an upstream identifier (from
// the Manifest). When the Upstream's routes are registered these middleware
// will be applied along with them. Middlewares are applied in-order.
func WithUpstreamMiddleware(upstream string, m ...func(http.Handler) http.Handler) MountOption {
	return func(c *mountConfig) {
		c.upstreamMiddleware[upstream] = m
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

// KeepTraliingSlashes forces trailing slashes to be unhandled. By default, the
// Proxy will strip trailing slashes where appropriate.
func KeepTrailingSlashes() MountOption {
	return func(c *mountConfig) {
		c.keepTrailingSlashes = true
	}
}

// mountConfig contains realized configuration for mounting routes.
type mountConfig struct {
	// Pass configuration
	observe             ObserveFunction
	root                string
	upstreamMiddleware  map[string][]func(http.Handler) http.Handler
	keepTrailingSlashes bool

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
		errorLog:           log.New(io.Discard, "", log.LstdFlags),
		upstreamMiddleware: map[string][]func(http.Handler) http.Handler{},
	}
}
