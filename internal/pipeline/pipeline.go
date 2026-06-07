// Package pipeline executes a job end-to-end: clone the repo, build the image,
// push it to the registry, render Kubernetes manifests, and apply them to the
// cluster. Each step updates the job's phase and streams logs into the store.
package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Dhruva430/vibsl/internal/docker"
	"github.com/Dhruva430/vibsl/internal/git"
	"github.com/Dhruva430/vibsl/internal/job"
	"github.com/Dhruva430/vibsl/internal/k8s"

	"k8s.io/apimachinery/pkg/runtime"
)

// Runner carries the dependencies needed to process jobs.
type Runner struct {
	Store   *job.Store
	Builder docker.Builder
	Applier *k8s.Applier // nil => dry-run (manifests rendered, not applied)
	WorkDir string
	Timeout time.Duration
	Now     func() time.Time
}

// Run executes the full pipeline for a single job id. It never returns an
// error to the caller; failures are recorded on the job record itself so the
// API surfaces them. The boolean reports overall success.
func (r *Runner) Run(parent context.Context, id string) bool {
	now := r.now
	ctx, cancel := context.WithTimeout(parent, r.Timeout)
	defer cancel()

	j, ok := r.Store.Get(id)
	if !ok {
		return false
	}
	spec := j.Spec

	// 1. Clone -----------------------------------------------------------
	r.Store.SetPhase(id, job.PhaseCloning, "cloning "+spec.RepoURL, now())
	checkout := filepath.Join(r.WorkDir, id)
	if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
		return r.fail(id, fmt.Errorf("prepare workdir: %w", err))
	}
	cloneLog := newLogWriter(r.Store, id, job.PhaseCloning, now)
	res, err := git.Clone(ctx, spec.RepoURL, spec.GitRef, checkout, cloneLog)
	if err != nil {
		return r.fail(id, err)
	}
	defer os.RemoveAll(checkout)
	r.Store.AppendLog(id, job.PhaseCloning, "checked out "+res.Commit, now())

	// 2. Build -----------------------------------------------------------
	imageRef := spec.Image.Ref()
	r.Store.Update(id, func(j *job.Job) { j.ImageRef = imageRef })
	r.Store.SetPhase(id, job.PhaseBuilding, "building "+imageRef, now())
	buildLog := newLogWriter(r.Store, id, job.PhaseBuilding, now)
	buildOpt := docker.BuildOptions{
		ContextDir: filepath.Join(checkout, spec.BuildContext),
		Dockerfile: spec.Dockerfile,
		ImageRef:   imageRef,
	}
	if err := r.Builder.Build(ctx, buildOpt, buildLog); err != nil {
		return r.fail(id, err)
	}

	// 3. Push ------------------------------------------------------------
	r.Store.SetPhase(id, job.PhasePushing, "pushing "+imageRef, now())
	pushLog := newLogWriter(r.Store, id, job.PhasePushing, now)
	if err := r.Builder.Push(ctx, imageRef, pushLog); err != nil {
		return r.fail(id, err)
	}

	// 4. Deploy ----------------------------------------------------------
	r.Store.SetPhase(id, job.PhaseDeploying, "rendering and applying manifests", now())
	objs := k8s.Generate(spec)
	deployLog := newLogWriter(r.Store, id, job.PhaseDeploying, now)
	if r.Applier == nil {
		r.renderOnly(id, objs, deployLog)
	} else {
		applied, err := r.Applier.ApplyAll(ctx, objs, deployLog)
		if err != nil {
			return r.fail(id, err)
		}
		r.Store.Update(id, func(j *job.Job) { j.Resources = applied })
	}

	r.Store.SetPhase(id, job.PhaseSucceeded, "deployed "+imageRef, now())
	return true
}

// renderOnly is the dry-run path: it records the generated object identifiers
// without contacting a cluster.
func (r *Runner) renderOnly(id string, objs []runtime.Object, logw *logWriter) {
	applied := make([]job.AppliedResource, 0, len(objs))
	for _, o := range objs {
		fmt.Fprintf(logw, "rendered %s (dry-run, not applied)\n", k8s.ObjectID(o))
		gvk := o.GetObjectKind().GroupVersionKind()
		if m, ok := o.(interface{ GetName() string }); ok {
			ns := ""
			if nn, ok := o.(interface{ GetNamespace() string }); ok {
				ns = nn.GetNamespace()
			}
			applied = append(applied, job.AppliedResource{Kind: gvk.Kind, Name: m.GetName(), Namespace: ns})
		}
	}
	r.Store.Update(id, func(j *job.Job) { j.Resources = applied })
}

// fail records a terminal failure on the job and returns false.
func (r *Runner) fail(id string, err error) bool {
	now := r.now()
	r.Store.Update(id, func(j *job.Job) {
		j.Phase = job.PhaseFailed
		j.Message = "failed: " + err.Error()
		j.Error = err.Error()
		j.FinishedAt = &now
		j.Logs = append(j.Logs, job.LogEntry{Time: now, Phase: job.PhaseFailed, Message: err.Error()})
	})
	return false
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}
