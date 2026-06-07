// Command server runs the queue-based Docker build and Kubernetes deploy API.
//
// It loads configuration from the environment, constructs the job store,
// pipeline, worker queue, and HTTP API, then serves until interrupted.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/Dhruva430/vibsl/internal/api"
	"github.com/Dhruva430/vibsl/internal/config"
	"github.com/Dhruva430/vibsl/internal/docker"
	"github.com/Dhruva430/vibsl/internal/job"
	"github.com/Dhruva430/vibsl/internal/k8s"
	"github.com/Dhruva430/vibsl/internal/pipeline"
	"github.com/Dhruva430/vibsl/internal/queue"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	cfg := config.Load()
	log.Printf("config: workers=%d queue=%d workdir=%s registry=%s dry_run_k8s=%t",
		cfg.Workers, cfg.QueueSize, cfg.WorkDir, cfg.DefaultRegistry, cfg.DryRunK8s)

	store := job.NewStore()

	// The Kubernetes applier is optional: in DRY_RUN_K8S mode (or if no cluster
	// is reachable) the pipeline renders manifests without applying them.
	var applier *k8s.Applier
	if !cfg.DryRunK8s {
		a, err := k8s.NewApplier(cfg.Kubeconfig, cfg.FieldManager)
		if err != nil {
			return err
		}
		applier = a
		log.Printf("kubernetes: server-side apply enabled (field manager %q)", cfg.FieldManager)
	} else {
		log.Printf("kubernetes: DRY_RUN_K8S=true, manifests will be rendered but not applied")
	}

	runner := &pipeline.Runner{
		Store:   store,
		Builder: docker.NewCLIBuilder(),
		Applier: applier,
		WorkDir: cfg.WorkDir,
		Timeout: cfg.JobTimeout,
	}

	q := queue.New(cfg.QueueSize, runner)

	// Root context cancelled on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	q.Start(ctx, cfg.Workers)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(store, q, cfg.DefaultRegistry).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server error: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Printf("shutdown signal received, draining...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	q.Drain()
	log.Printf("goodbye")
	return nil
}
