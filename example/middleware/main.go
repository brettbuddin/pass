package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/brettbuddin/pass"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	var manifestPath string
	fs := flag.NewFlagSet("pass-middleware", flag.ExitOnError)
	fs.StringVar(&manifestPath, "manifest", "", "HCL manifest path")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	if manifestPath == "" {
		return fmt.Errorf("--manifest is required")
	}

	m, err := pass.LoadManifest(manifestPath, nil)
	if err != nil {
		return err
	}

	// Build a stack of arbitrary middleware that will be applied to an Upstream
	// proxy. These middleware will be applied in the order they are defined and
	// presented to the pass.WithMiddleware option.
	middlewares := []func(next http.Handler) http.Handler{
		middleware("A"),
		middleware("B"),
		middleware("C"),
	}

	// Apply the middleware stack to each Upstream in the Manifest.
	var opts []pass.MountOption
	for _, u := range m.Upstreams {
		opts = append(opts, pass.WithUpstreamMiddleware(u.Identifier, middlewares...))
	}

	proxy, err := pass.New(m, opts...)
	if err != nil {
		return err
	}
	return http.ListenAndServe(":8080", proxy)
}

func middleware(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Middleware-"+name, name)
			next.ServeHTTP(w, r)
		})
	}
}
