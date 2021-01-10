package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/brettbuddin/pass"
	"github.com/brettbuddin/pass/passutil"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	var manifestPath string
	fs := flag.NewFlagSet("pass-stdlib", flag.ExitOnError)
	fs.StringVar(&manifestPath, "manifest", "", "HCL manifest path")
	if err := fs.Parse(os.Args); err != nil {
		return err
	}

	if manifestPath == "" {
		return fmt.Errorf("--manifest is required")
	}

	m, err := pass.LoadManifest(manifestPath, nil)
	if err != nil {
		return err
	}

	mux := passutil.NewServeMux(http.NewServeMux())
	if err = pass.Mount(m, mux); err != nil {
		return err
	}
	return http.ListenAndServe(":8080", mux)
}
