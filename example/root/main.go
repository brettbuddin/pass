package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/brettbuddin/pass"
	"github.com/go-chi/chi"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	var manifestPath string
	fs := flag.NewFlagSet("pass-root", flag.ExitOnError)
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

	var (
		r    chi.Router = chi.NewRouter()
		merr error
	)
	r = r.Route("/api/v2", func(r chi.Router) {
		// Notify the proxy about the root mount point. This root prefix will be
		// stripped from the path of outgoing requests.
		merr = pass.Mount(m, r, pass.WithRoot("/api/v2"))
	})
	if merr != nil {
		return merr
	}
	return http.ListenAndServe(":8080", r)
}
