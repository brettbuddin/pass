package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/brettbuddin/pass"
	"github.com/go-chi/chi"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	var manifestPath string
	fs := flag.NewFlagSet("pass-observe", flag.ExitOnError)
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

	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}

	r := chi.NewRouter()

	// Specify and pass a pass.ObserveFunction to perform some logging of the
	// requests passed upstream.
	observe := func(r *http.Request, info *pass.RouteInfo) {
		logger.Info("Proxying",
			zap.String("host", info.UpstreamHost),
			zap.String("owner", info.UpstreamOwner),
			zap.String("method", info.RouteMethod),
			zap.String("path", info.RoutePath))
	}
	err = pass.Mount(m, r, pass.WithObserveFunction(observe))
	if err != nil {
		return err
	}

	return http.ListenAndServe(":8080", r)
}
