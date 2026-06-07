// Command render reads a job spec (JSON) from a file or stdin and prints the
// generated Kubernetes manifests as YAML, without building or deploying
// anything. It is handy for inspecting what a job would create and for
// generating the sample manifests checked into examples/.
//
// Usage:
//
//	render examples/job.json
//	cat examples/job.json | render
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Dhruva430/vibsl/internal/job"
	"github.com/Dhruva430/vibsl/internal/k8s"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var in io.Reader = os.Stdin
	if len(os.Args) > 1 {
		f, err := os.Open(os.Args[1])
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}

	var spec job.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("parse job spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return err
	}

	// Use a deterministic tag so generated samples are stable across runs.
	defaulted := spec.WithDefaults("localhost:5001", "v1")
	yaml, err := k8s.RenderYAML(k8s.Generate(defaulted))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(yaml)
	return err
}
