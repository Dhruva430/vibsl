package job

import (
	"sort"
	"sync"
	"time"
)

// Store is a concurrency-safe, in-memory collection of jobs. It is the single
// source of truth that the API reads from and the pipeline writes to.
//
// The implementation is deliberately simple (a guarded map). Swapping it for a
// database means satisfying the same method set; see README "Limitations".
type Store struct {
	mu   sync.RWMutex
	jobs map[string]*Job
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{jobs: make(map[string]*Job)}
}

// Add inserts a new job. The caller owns id generation.
func (s *Store) Add(j *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
}

// Get returns a deep-enough copy of the job by id. The bool reports existence.
// Returning a copy keeps callers from mutating stored state without Update.
func (s *Store) Get(id string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	return j.clone(), true
}

// List returns all jobs, newest first.
func (s *Store) List() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j.clone())
	}
	sort.Slice(out, func(i, k int) bool {
		return out[i].CreatedAt.After(out[k].CreatedAt)
	})
	return out
}

// Update applies mut to the stored job under the lock, so concurrent pipeline
// steps and API reads never observe a half-written record.
func (s *Store) Update(id string, mut func(*Job)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		mut(j)
	}
}

// SetPhase is a convenience wrapper that transitions phase, records a message,
// appends a structured log line, and stamps lifecycle timestamps.
func (s *Store) SetPhase(id string, p Phase, msg string, now time.Time) {
	s.Update(id, func(j *Job) {
		j.Phase = p
		j.Message = msg
		if p != PhasePending && j.StartedAt == nil {
			t := now
			j.StartedAt = &t
		}
		if p.IsTerminal() {
			t := now
			j.FinishedAt = &t
		}
		j.Logs = append(j.Logs, LogEntry{Time: now, Phase: p, Message: msg})
	})
}

// AppendLog adds a structured log line without changing phase.
func (s *Store) AppendLog(id string, p Phase, msg string, now time.Time) {
	s.Update(id, func(j *Job) {
		j.Logs = append(j.Logs, LogEntry{Time: now, Phase: p, Message: msg})
	})
}

// clone returns a copy safe to hand out to readers. Slices and maps are copied
// so external mutation cannot race the store's internal state.
func (j *Job) clone() Job {
	cp := *j
	if j.Logs != nil {
		cp.Logs = append([]LogEntry(nil), j.Logs...)
	}
	if j.Resources != nil {
		cp.Resources = append([]AppliedResource(nil), j.Resources...)
	}
	if j.StartedAt != nil {
		t := *j.StartedAt
		cp.StartedAt = &t
	}
	if j.FinishedAt != nil {
		t := *j.FinishedAt
		cp.FinishedAt = &t
	}
	return cp
}
