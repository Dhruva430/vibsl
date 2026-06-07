// Package api exposes the job HTTP surface: submit jobs, query their status,
// list them, and a health probe. It depends only on the job store and the
// queue, keeping transport concerns separate from pipeline logic.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Dhruva430/vibsl/internal/job"
	"github.com/Dhruva430/vibsl/internal/queue"
)

// Server wires the HTTP handlers to their dependencies.
type Server struct {
	store           *job.Store
	queue           *queue.Queue
	defaultRegistry string
	now             func() time.Time
}

// New constructs a Server.
func New(store *job.Store, q *queue.Queue, defaultRegistry string) *Server {
	return &Server{
		store:           store,
		queue:           q,
		defaultRegistry: defaultRegistry,
		now:             func() time.Time { return time.Now().UTC() },
	}
}

// Routes returns an http.Handler with all endpoints registered. Go 1.22+
// pattern routing gives us method + path-parameter matching without a router
// dependency.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", s.createJob)
	mux.HandleFunc("GET /jobs", s.listJobs)
	mux.HandleFunc("GET /jobs/{job_id}", s.getJob)
	mux.HandleFunc("GET /healthz", s.healthz)
	return logging(mux)
}

// createJob handles POST /jobs: validate, default, store as pending, enqueue.
func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var spec job.Spec
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := spec.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id := newID()
	defaulted := spec.WithDefaults(s.defaultRegistry, shortID(id))
	now := s.now()
	j := &job.Job{
		ID:        id,
		Spec:      defaulted,
		Phase:     job.PhasePending,
		Message:   "queued",
		CreatedAt: now,
		Logs:      []job.LogEntry{{Time: now, Phase: job.PhasePending, Message: "queued"}},
	}
	s.store.Add(j)

	if !s.queue.Enqueue(id) {
		s.store.SetPhase(id, job.PhaseFailed, "queue is full, rejected", s.now())
		writeError(w, http.StatusServiceUnavailable, "queue is full, try again later")
		return
	}

	stored, _ := s.store.Get(id)
	writeJSON(w, http.StatusAccepted, stored)
}

// getJob handles GET /jobs/{job_id}.
func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("job_id")
	j, ok := s.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// listJobs handles GET /jobs, newest first.
func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.store.List()})
}

// healthz handles GET /healthz.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"queue_depth": s.queue.Depth(),
		"time":        s.now(),
	})
}

// --- helpers --------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// newID returns a random 16-byte hex id for a job.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// shortID is the first 8 hex chars, used as a default image tag.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}
