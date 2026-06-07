// Package docker drives image builds and pushes. The default implementation
// shells out to the `docker` CLI, which transparently uses BuildKit and works
// with any Dockerfile a user might bring. The Builder interface lets tests (or
// a future Docker-SDK/buildkit implementation) substitute the behaviour.
package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// Builder builds an image from a context directory and pushes it to a registry.
type Builder interface {
	Build(ctx context.Context, opt BuildOptions, logw io.Writer) error
	Push(ctx context.Context, imageRef string, logw io.Writer) error
}

// BuildOptions describes a single image build.
type BuildOptions struct {
	ContextDir string // absolute path to the build context
	Dockerfile string // path to the Dockerfile, relative to ContextDir
	ImageRef   string // fully-qualified tag to apply, e.g. localhost:5001/app:v1
}

// CLIBuilder implements Builder by invoking the local docker binary.
type CLIBuilder struct {
	// Bin is the docker executable; defaults to "docker".
	Bin string
	// buildKit indicates whether the BuildKit/buildx backend is available. When
	// false we explicitly select the legacy builder, since modern docker
	// defaults to BuildKit and errors hard if buildx is missing.
	buildKit bool
}

// NewCLIBuilder returns a CLIBuilder using the docker binary on PATH, probing
// once for buildx so builds work whether or not the plugin is installed.
func NewCLIBuilder() *CLIBuilder {
	b := &CLIBuilder{Bin: "docker"}
	b.buildKit = exec.Command(b.bin(), "buildx", "version").Run() == nil
	return b
}

func (b *CLIBuilder) bin() string {
	if b.Bin == "" {
		return "docker"
	}
	return b.Bin
}

// Build runs `docker build -f <dockerfile> -t <ref> <context>`.
func (b *CLIBuilder) Build(ctx context.Context, opt BuildOptions, logw io.Writer) error {
	dockerfile := opt.Dockerfile
	if !filepath.IsAbs(dockerfile) {
		dockerfile = filepath.Join(opt.ContextDir, opt.Dockerfile)
	}
	args := []string{
		"build",
		"--file", dockerfile,
		"--tag", opt.ImageRef,
		opt.ContextDir,
	}
	return run(ctx, b.bin(), args, b.buildKitEnv(), logw)
}

// Push runs `docker push <ref>`.
func (b *CLIBuilder) Push(ctx context.Context, imageRef string, logw io.Writer) error {
	return run(ctx, b.bin(), []string{"push", imageRef}, nil, logw)
}

// buildKitEnv selects the build backend explicitly so behaviour is stable
// regardless of the docker default.
func (b *CLIBuilder) buildKitEnv() []string {
	if b.buildKit {
		return []string{"DOCKER_BUILDKIT=1"}
	}
	return []string{"DOCKER_BUILDKIT=0"}
}

// run executes a command, streaming combined stdout/stderr to logw and
// surfacing a useful error (the build backend prints the real failure there).
func run(ctx context.Context, bin string, args, extraEnv []string, logw io.Writer) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = logw
	cmd.Stderr = logw
	cmd.Env = append(cmd.Environ(), extraEnv...)
	fmt.Fprintf(logw, "$ %s %s\n", bin, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", bin, args[0], err)
	}
	return nil
}
