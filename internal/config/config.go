// Package config holds runtime configuration for the deploy service, all
// sourced from environment variables so the binary stays 12-factor friendly.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	// HTTPAddr is the listen address for the API server, e.g. ":8080".
	HTTPAddr string

	// Workers is the number of concurrent pipeline workers draining the queue.
	Workers int

	// QueueSize is the buffered capacity of the in-memory job queue.
	QueueSize int

	// WorkDir is the parent directory under which per-job checkouts are made.
	WorkDir string

	// Kubeconfig is an explicit path to a kubeconfig file. When empty the
	// client falls back to KUBECONFIG / ~/.kube/config / in-cluster config.
	Kubeconfig string

	// DefaultRegistry is used when a job does not specify image.registry.
	DefaultRegistry string

	// DryRunK8s, when true, renders manifests but does not apply them to a
	// cluster. Useful for environments without a reachable API server.
	DryRunK8s bool

	// JobTimeout bounds the wall-clock time of a single pipeline run.
	JobTimeout time.Duration

	// FieldManager is the server-side-apply field manager name.
	FieldManager string
}

// Load builds a Config from the environment, applying sensible defaults.
func Load() Config {
	return Config{
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		Workers:         envInt("WORKERS", 2),
		QueueSize:       envInt("QUEUE_SIZE", 64),
		WorkDir:         env("WORK_DIR", os.TempDir()+"/vibsl-jobs"),
		Kubeconfig:      env("KUBECONFIG", ""),
		DefaultRegistry: env("DEFAULT_REGISTRY", "localhost:5001"),
		DryRunK8s:       envBool("DRY_RUN_K8S", false),
		JobTimeout:      time.Duration(envInt("JOB_TIMEOUT_SECONDS", 900)) * time.Second,
		FieldManager:    env("FIELD_MANAGER", "vibsl-controller"),
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
