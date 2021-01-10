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

	"github.com/go-chi/chi"
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

	expect := &Manifest{
		PrefixPath: "/api/v2",
		Upstreams: []Upstream{
			{
				Identifier:      "widgets",
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
			},
			{
				Identifier:      "bobs",
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
			},
		},
	}

	require.Equal(t, expect, m)
}

func TestRouting(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.URL.Path)
	}))
	defer destination.Close()

	m := &Manifest{
		PrefixPath: "/api/v2",
		Upstreams: []Upstream{
			{
				Identifier:  "accounts",
				Owner:       "Identity <team-identity@company.com>",
				Destination: destination.URL,
				PrefixPath:  "/private",
				Routes: []Route{
					{
						Methods: []string{http.MethodGet},
						Path:    "/accounts/{id}",
					},
					{
						Methods: []string{http.MethodGet},
						Path:    "/accounts",
					},
				},
			},
		},
	}

	t.Run("plain route", func(t *testing.T) {
		r := chi.NewRouter()
		err := Mount(m, r)
		require.NoError(t, err)
		server := httptest.NewServer(r)
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
		r := chi.NewRouter()
		err := Mount(m, r)
		require.NoError(t, err)
		server := httptest.NewServer(r)
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
		r := chi.NewRouter()
		err := Mount(m, r)
		require.NoError(t, err)
		server := httptest.NewServer(r)
		defer server.Close()
		client := &http.Client{Timeout: 1 * time.Second}

		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v2/private/notfound", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("with root specified", func(t *testing.T) {
		var r chi.Router = chi.NewRouter()
		r = r.Route("/root", func(r chi.Router) {
			err := Mount(m, r, WithRoot("/root"))
			require.NoError(t, err)
		})
		server := httptest.NewServer(r)
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

		r := chi.NewRouter()
		err := Mount(m, r, WithObserveFunction(observe))
		require.NoError(t, err)
		server := httptest.NewServer(r)
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
}

func TestErrorLogging(t *testing.T) {
	b := bytes.NewBuffer(nil)
	l := log.New(b, "", 0)
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("broken transport")
	})

	r := chi.NewRouter()
	m := manifest("http://badhost.local")
	err := Mount(m, r,
		WithErrorLog(l),
		WithTransport(transport),
	)
	require.NoError(t, err)

	server := httptest.NewServer(r)
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

	r := chi.NewRouter()
	m := manifest("http://badhost.local")
	err := Mount(m, r,
		WithErrorHandler(errorHandler),
		WithTransport(transport),
	)
	require.NoError(t, err)

	server := httptest.NewServer(r)
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

	r := chi.NewRouter()
	m := manifest(destination.URL)
	err := Mount(m, r,
		WithRequestModifier(func(r *http.Request) {
			r.Header.Add("Direction", "in")
		}),
		WithResponseModifier(func(r *http.Response) error {
			r.Header.Add("Direction", "out")
			return nil
		}),
	)
	require.NoError(t, err)

	server := httptest.NewServer(r)
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

func TestMissingSchema(t *testing.T) {
	m := manifest("noschema.local")
	err := Mount(m, chi.NewRouter())
	require.Error(t, err)
	require.Equal(t, err.Error(), `missing scheme: "noschema.local"`)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func manifest(destination string) *Manifest {
	return &Manifest{
		Upstreams: []Upstream{
			{
				Identifier:  "accounts",
				Owner:       "Identity <team-identity@company.com>",
				Destination: destination,
				Routes: []Route{
					{
						Methods: []string{http.MethodGet},
						Path:    "/accounts",
					},
				},
			},
		},
	}
}
