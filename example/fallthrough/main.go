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
	fs := flag.NewFlagSet("pass-fallthrough", flag.ExitOnError)
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

	// Set up the router so that the routes specified in the manifest (proxy)
	// take precedence over routes defined in this process (mux). This is a
	// useful pattern if your are trying to gradually offload certain traffic
	// from one host to another.
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from an in-process route")
	})
	proxy, err := pass.New(m, pass.WithNotFound(mux.ServeHTTP))
	if err != nil {
		return err
	}
	return http.ListenAndServe(":8080", proxy)
}
