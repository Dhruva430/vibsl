package pipeline

import (
	"bytes"
	"sync"
	"time"

	"github.com/Dhruva430/vibsl/internal/job"
)

// logWriter is an io.Writer that turns streamed command output into structured
// per-line log entries on a job. It buffers partial lines until a newline so
// multi-write log lines are not split mid-message.
type logWriter struct {
	store *job.Store
	id    string
	phase job.Phase
	now   func() time.Time

	mu  sync.Mutex
	buf bytes.Buffer
}

func newLogWriter(store *job.Store, id string, phase job.Phase, now func() time.Time) *logWriter {
	return &logWriter{store: store, id: id, phase: phase, now: now}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// No full line yet; put the remainder back and wait for more.
			w.buf.Reset()
			w.buf.WriteString(line)
			break
		}
		trimmed := trimEOL(line)
		if trimmed != "" {
			w.store.AppendLog(w.id, w.phase, trimmed, w.now())
		}
	}
	return len(p), nil
}

func trimEOL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
