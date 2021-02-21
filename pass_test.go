package pass

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestParsing(t *testing.T) {
	ectx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"namespace": cty.StringVal("primary"),
		},
	}
	m, err := LoadManifest("testdata/manifest.hcl", ectx)
	require.NoError(t, err)

	widgets := Upstream{
		Identifier: "widgets",
		Annotations: map[string]string{
			"company/middleware-stack": "jwt",
		},
		Destination:     "http://widgets.primary.local",
		Owner:           "Team A <team-a@company.com>",
		FlushIntervalMS: 0,
		PrefixPath:      "/private",
		Routes: []Route{
			{
				Methods: []string{http.MethodGet},
				Path:    "/widgets",
			},
		},
	}
	bobs := Upstream{
		Identifier:      "bobs",
		Annotations:     map[string]string{},
		Destination:     "http://bobs.primary.local",
		Owner:           "Team B <team-b@company.com>",
		FlushIntervalMS: 1000,
		Routes: []Route{
			{
				Methods: []string{http.MethodGet},
				Path:    "/bobs/{[0-9]+}",
			},
			{
				Methods: []string{http.MethodGet, http.MethodPost},
				Path:    "/bobs",
			},
		},
	}

	expect := Manifest{
		PrefixPath: "/api/v2",
		Annotations: map[string]string{
			"company/version": "1",
		},
		Upstreams: []Upstream{widgets, bobs},
	}

	diff := cmp.Diff(expect, *m,
		cmpopts.IgnoreUnexported(expect))
	require.Empty(t, diff)
}

func TestRouting(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.URL.Path)
	}))
	defer destination.Close()

	ectx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"destination": cty.StringVal(destination.URL),
		},
	}
	m, err := LoadManifest("testdata/routing.hcl", ectx)
	require.NoError(t, err)

	t.Run("plain route", func(t *testing.T) {
		proxy, err := New(m)
		require.NoError(t, err)
		server := httptest.NewServer(proxy)
		defer server.Close()
		client := &http.Client{Timeout: 1 * time.Second}

		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v2/private/accounts", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "/accounts", string(b))
	})

	t.Run("parameterized route", func(t *testing.T) {
		proxy, err := New(m)
		require.NoError(t, err)
		server := httptest.NewServer(proxy)
		defer server.Close()
		client := &http.Client{Timeout: 1 * time.Second}

		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v2/private/accounts/1", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "/accounts/1", string(b))
	})

	t.Run("not found", func(t *testing.T) {
		proxy, err := New(m)
		require.NoError(t, err)
		server := httptest.NewServer(proxy)
		defer server.Close()
		client := &http.Client{Timeout: 1 * time.Second}

		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v2/private/notfound", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("with root specified", func(t *testing.T) {
		proxy, err := New(m, WithRoot("/root"))
		require.NoError(t, err)
		require.Equal(t, "/root/api/v2", proxy.Root())

		server := httptest.NewServer(proxy)
		defer server.Close()
		client := &http.Client{Timeout: 1 * time.Second}

		req, err := http.NewRequest(http.MethodGet, server.URL+"/root/api/v2/private/accounts", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		b, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "/accounts", string(b))
	})

	t.Run("with observe function", func(t *testing.T) {
		var captured *RouteInfo
		observe := func(r *http.Request, info *RouteInfo) {
			captured = info
		}

		proxy, err := New(m, WithObserveFunction(observe))
		require.NoError(t, err)
		server := httptest.NewServer(proxy)
		defer server.Close()
		client := &http.Client{Timeout: 1 * time.Second}

		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v2/private/accounts", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		require.Equal(t, &RouteInfo{
			RouteMethod:        http.MethodGet,
			RoutePath:          "/accounts",
			RoutePrefix:        "/api/v2/private",
			UpstreamHost:       destination.URL,
			UpstreamIdentifier: "accounts",
			UpstreamOwner:      "Identity <team-identity@company.com>",
		}, captured)
	})

	t.Run("upstreams exposed", func(t *testing.T) {
		proxy, err := New(m)
		require.NoError(t, err)
		require.Equal(t, m.Upstreams, proxy.Upstreams())
	})

	t.Run("middleware", func(t *testing.T) {
		a := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Middleware-A", "value")
				next.ServeHTTP(w, r)
			})
		}
		b := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Middleware-B", "value")
				next.ServeHTTP(w, r)
			})
		}

		proxy, err := New(m, WithUpstreamMiddleware("accounts", a, b))
		require.NoError(t, err)
		server := httptest.NewServer(proxy)
		defer server.Close()
		client := &http.Client{Timeout: 1 * time.Second}

		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v2/private/accounts", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "value", resp.Header.Get("Middleware-A"), "middleware A header missing")
		require.Equal(t, "value", resp.Header.Get("Middleware-B"), "middleware B header missing")
	})
}

func TestErrorLogging(t *testing.T) {
	b := bytes.NewBuffer(nil)
	l := log.New(b, "", 0)
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("broken transport")
	})

	ectx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"destination": cty.StringVal("http://badhost.local"),
		},
	}
	m, err := LoadManifest("testdata/basic_destination.hcl", ectx)
	require.NoError(t, err)
	proxy, err := New(m,
		WithErrorLog(l),
		WithTransport(transport),
	)
	require.NoError(t, err)

	server := httptest.NewServer(proxy)
	defer server.Close()
	client := &http.Client{Timeout: 500 * time.Millisecond}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/accounts", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	require.Contains(t, b.String(), "broken transport")
}

func TestErrorHandling(t *testing.T) {
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("broken transport")
	})

	var capturedErr error
	errorHandler := func(w http.ResponseWriter, r *http.Request, err error) {
		w.WriteHeader(http.StatusTeapot)
		capturedErr = err
	}

	ectx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"destination": cty.StringVal("http://badhost.local"),
		},
	}
	m, err := LoadManifest("testdata/basic_destination.hcl", ectx)
	require.NoError(t, err)
	proxy, err := New(m,
		WithErrorHandler(errorHandler),
		WithTransport(transport),
	)
	require.NoError(t, err)

	server := httptest.NewServer(proxy)
	defer server.Close()
	client := &http.Client{Timeout: 500 * time.Millisecond}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/accounts", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusTeapot, resp.StatusCode)
	require.Error(t, capturedErr)
}

func TestModification(t *testing.T) {
	var requestHeader string
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHeader = r.Header.Get("Direction")
	}))
	defer destination.Close()

	ectx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"destination": cty.StringVal(destination.URL),
		},
	}
	m, err := LoadManifest("testdata/basic_destination.hcl", ectx)
	require.NoError(t, err)
	proxy, err := New(m,
		WithRequestModifier(func(r *http.Request) {
			r.Header.Add("Direction", "in")
		}),
		WithResponseModifier(func(r *http.Response) error {
			r.Header.Add("Direction", "out")
			return nil
		}),
	)
	require.NoError(t, err)

	server := httptest.NewServer(proxy)
	defer server.Close()
	client := &http.Client{Timeout: 500 * time.Millisecond}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/accounts", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "out", resp.Header.Get("Direction"))
	require.Equal(t, "in", requestHeader)
}

func TestMissingScheme(t *testing.T) {
	ectx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"destination": cty.StringVal("noscheme.local"),
		},
	}
	m, err := LoadManifest("testdata/basic_destination.hcl", ectx)
	require.NoError(t, err)
	_, err = New(m)
	require.Error(t, err)
	require.Equal(t, err.Error(), `missing scheme: "noscheme.local"`)
}

func TestEmptyRoot(t *testing.T) {
	m := &Manifest{}
	proxy, err := New(m)
	require.NoError(t, err)
	require.Equal(t, "/", proxy.Root())
}

func TestDuplicateUpstreamIdentifier(t *testing.T) {
	_, err := LoadManifest("testdata/duplicate_identifier.hcl", nil)
	require.Error(t, err)
	require.Equal(t, `duplicate upstream identifier: "widgets"`, err.Error())
}

func TestMissingUpstreamForMiddleware(t *testing.T) {
	m, err := LoadManifest("testdata/basic.hcl", nil)
	require.NoError(t, err)
	_, err = New(m, WithUpstreamMiddleware("doesnt-exist", nil))
	require.Error(t, err)
	require.Equal(t, `upstream missing for middleware stack: "doesnt-exist"`, err.Error())
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
