// Package git wraps the bits of go-git we need: a shallow clone of a single
// ref into a job-scoped working directory. Using go-git keeps the clone step
// pure-Go (no dependency on a git binary in the runtime image).
package git

import (
	"context"
	"fmt"
	"io"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// CloneResult reports what was actually checked out.
type CloneResult struct {
	Dir    string // absolute path of the checkout
	Commit string // resolved HEAD commit sha
	Ref    string // the ref that was requested ("" => default HEAD)
}

// Clone performs a shallow (depth=1) clone of repoURL@ref into dir, streaming
// progress to logw. When ref is empty the remote's default branch is used.
func Clone(ctx context.Context, repoURL, ref, dir string, logw io.Writer) (*CloneResult, error) {
	opts := &gogit.CloneOptions{
		URL:               repoURL,
		Depth:             1,
		SingleBranch:      true,
		RecurseSubmodules: gogit.NoRecurseSubmodules,
		Progress:          logw,
	}
	// A ref may be a branch or a tag; try branch semantics first, which is the
	// common case, and fall back to a plain reference name.
	if ref != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(ref)
	}

	repo, err := gogit.PlainCloneContext(ctx, dir, false, opts)
	if err != nil && ref != "" {
		// Retry treating the ref as a tag before giving up.
		opts.ReferenceName = plumbing.NewTagReferenceName(ref)
		repo, err = gogit.PlainCloneContext(ctx, dir, false, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("clone %s: %w", repoURL, err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}

	return &CloneResult{Dir: dir, Commit: head.Hash().String(), Ref: ref}, nil
}
