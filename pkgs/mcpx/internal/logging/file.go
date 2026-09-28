package logging

import (
	"encoding/json"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// FileOptions configure the durable log.
type FileOptions struct {
	// Dir holds the log files.
	Dir string
	// MaxBytes rotates the active file once it exceeds this size. Zero uses
	// the default.
	MaxBytes int64
	// Keep is how many rotated files to retain. Zero uses the default.
	Keep int
	// Level is the minimum level written to disk, which is deliberately more
	// generous than the terminal's: a file is cheap and a question asked
	// tomorrow cannot be answered by a line that was never written.
	Level string
}

const ()

// FileSink appends records to a dated file as JSON lines.
//
// JSON regardless of the terminal format, because a file is read by tools and
// a terminal is read by a person. Writes are buffered only by the OS: a crash
// that loses the last few lines is exactly when those lines mattered, and the
// volume here does not justify a flush loop.
type FileSink struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	keep     int
	min      Level
	day      string
	file     *os.File
	written  int64
	failed   bool
}

// Level is re-exported so callers need not import log/slog for the common case.
type Level = int

// NewFileSink opens the current day's file, creating the directory.
func NewFileSink(opts FileOptions) (*FileSink, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("log directory is empty")
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, err
	}
	s := &FileSink{
		dir:      opts.Dir,
		maxBytes: opts.MaxBytes,
		keep:     opts.Keep,
	}
	if s.maxBytes <= 0 {
		s.maxBytes = defaults.LogMaxBytes
	}
	if s.keep <= 0 {
		s.keep = defaults.LogKeep
	}
	if err := s.reopen(time.Now()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileSink) path(day string) string {
	return filepath.Join(s.dir, "mcpx-"+day+".jsonl")
}

func (s *FileSink) reopen(now time.Time) error {
	if s.file != nil {
		s.file.Close()
	}
	s.day = now.Format("2006-01-02")
	f, err := os.OpenFile(s.path(s.day), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	s.file = f
	if st, err := f.Stat(); err == nil {
		s.written = st.Size()
	}
	return nil
}

// Write appends one record.
func (s *FileSink) Write(r Record, extra map[string]any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed || s.file == nil {
		return
	}

	now := r.Time
	if now.IsZero() {
		now = time.Now()
	}
	if day := now.Format("2006-01-02"); day != s.day {
		if err := s.reopen(now); err != nil {
			s.failed = true
			return
		}
	}
	if s.written >= s.maxBytes {
		if err := s.rotate(now); err != nil {
			s.failed = true
			return
		}
	}

	obj := map[string]any{
		"ts":    now.Format(time.RFC3339Nano),
		"level": LevelName(r.Level),
		"msg":   r.Msg,
	}
	if r.Template != "" && r.Template != r.Msg {
		obj["template"] = r.Template
	}
	mergeAttrs(obj, r.Attrs)
	mergeAttrs(obj, extra)

	b, err := json.Marshal(obj)
	if err != nil {
		return
	}
	b = append(b, '\n')
	n, werr := s.file.Write(b)
	s.written += int64(n)
	if werr != nil {
		// A full disk should not take the daemon with it; stop writing and
		// leave the terminal output working.
		s.failed = true
	}
}

// rotate renames the active file aside and prunes the oldest.
func (s *FileSink) rotate(now time.Time) error {
	s.file.Close()
	stamp := now.Format("150405")
	if err := os.Rename(s.path(s.day), filepath.Join(s.dir,
		fmt.Sprintf("mcpx-%s-%s.jsonl", s.day, stamp))); err != nil {
		return err
	}
	s.prune()
	return s.reopen(now)
}

func (s *FileSink) prune() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	var rotated []string
	for _, e := range entries {
		name := e.Name()
		// Only files carrying a rotation stamp are pruned; a day's live file
		// is never removed out from under a reader.
		if len(name) > len("mcpx-2006-01-02.jsonl") && filepath.Ext(name) == ".jsonl" {
			rotated = append(rotated, name)
		}
	}
	if len(rotated) <= s.keep {
		return
	}
	sort.Strings(rotated)
	for _, name := range rotated[:len(rotated)-s.keep] {
		os.Remove(filepath.Join(s.dir, name))
	}
}

// Close releases the file.
func (s *FileSink) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

// Path is the file currently being written, for diagnostics.
func (s *FileSink) Path() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path(s.day)
}
