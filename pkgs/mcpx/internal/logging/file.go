package logging

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// FileOptions configure the durable log.
//
// The three rotation thresholds answer three different questions and none
// subsumes the others. Bytes bound what a single read costs. Lines bound what
// an ingest costs, and are not proportional to bytes once one record carries a
// large payload. Age bounds how far back the active file reaches, which is
// what makes "the last day" a thing you can point at rather than a thing you
// have to search for.
type FileOptions struct {
	// Dir holds the log files.
	Dir string
	// MaxBytes rotates the active file once it exceeds this size. Zero uses
	// the default.
	MaxBytes int64
	// MaxLines rotates the active file once it holds this many records. Zero
	// uses the default; negative disables the trigger.
	MaxLines int64
	// MaxAge rotates the active file once its oldest record is this old. Zero
	// uses the default; negative disables the trigger.
	MaxAge time.Duration
	// Keep is how many rotated files to retain. Zero uses the default.
	Keep int
	// Level is the minimum level written to disk, which is deliberately more
	// generous than the terminal's: a file is cheap and a question asked
	// tomorrow cannot be answered by a line that was never written.
	Level string
}

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
	maxLines int64
	maxAge   time.Duration
	keep     int
	min      Level
	day      string
	file     *os.File
	written  int64
	lines    int64
	// oldest is the timestamp of the first record in the active file, which is
	// what the age threshold is measured against.
	oldest time.Time
	failed bool
}

// Level is re-exported so callers need not import log/slog for the common case.
type Level = int

// NewFileSink opens the current day's file, creating the directory.
func NewFileSink(opts FileOptions) (*FileSink, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("log directory is empty")
	}
	if err := os.MkdirAll(opts.Dir, defaults.DirMode); err != nil {
		return nil, err
	}
	s := &FileSink{
		dir:      opts.Dir,
		maxBytes: opts.MaxBytes,
		maxLines: opts.MaxLines,
		maxAge:   opts.MaxAge,
		keep:     opts.Keep,
	}
	if s.maxBytes <= 0 {
		s.maxBytes = defaults.LogMaxBytes
	}
	if s.maxLines == 0 {
		s.maxLines = defaults.LogMaxLines
	}
	if s.maxAge == 0 {
		s.maxAge = defaults.LogMaxAge
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
	path := s.path(s.day)
	// Whatever is already on disk counts against the thresholds, or a daemon
	// restarted every few minutes would never rotate at all.
	s.lines, s.oldest = surveyLog(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, defaults.PrivateMode)
	if err != nil {
		return err
	}
	s.file = f
	s.written = 0
	if st, err := f.Stat(); err == nil {
		s.written = st.Size()
	}
	return nil
}

// surveyLog counts the records already in a file and reads the timestamp of
// the first one.
//
// The age of a log file is the age of its oldest record, and that is the only
// portable way to get it: a file's birth time needs a per-OS syscall, and its
// mtime is the last write, which says nothing about how far back the contents
// reach. The scan is bounded by MaxBytes, so it is a few milliseconds once per
// process start.
func surveyLog(path string) (lines int64, oldest time.Time) {
	f, err := os.Open(path)
	if err != nil {
		return 0, time.Time{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		lines++
		if !oldest.IsZero() {
			continue
		}
		var head struct {
			TS string `json:"ts"`
		}
		if json.Unmarshal(sc.Bytes(), &head) == nil {
			if t, terr := time.Parse(time.RFC3339Nano, head.TS); terr == nil {
				oldest = t
			}
		}
	}
	return lines, oldest
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
	if s.written >= s.maxBytes || s.overLines() || s.overAge(now) {
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
	// The age of the file is the age of its first record, so an empty file
	// takes its clock from whatever lands in it rather than from when it was
	// opened. A daemon that idles for a day then logs once would otherwise
	// rotate immediately and leave a one-line file behind.
	if s.lines == 0 {
		s.oldest = now
	}
	b = append(b, '\n')
	n, werr := s.file.Write(b)
	s.written += int64(n)
	s.lines++
	if werr != nil {
		// A full disk should not take the daemon with it; stop writing and
		// leave the terminal output working.
		s.failed = true
	}
}

func (s *FileSink) overLines() bool {
	return s.maxLines > 0 && s.lines >= s.maxLines
}

// overAge is measured against the oldest record rather than wall-clock since
// open, so a file that was already half a day old when this process started
// still rotates on schedule.
func (s *FileSink) overAge(now time.Time) bool {
	return s.maxAge > 0 && !s.oldest.IsZero() && now.Sub(s.oldest) >= s.maxAge
}

// rotate renames the active file aside and prunes the oldest.
func (s *FileSink) rotate(now time.Time) error {
	s.file.Close()
	if err := os.Rename(s.path(s.day), s.rotatedPath(now)); err != nil {
		return err
	}
	s.prune()
	return s.reopen(now)
}

// rotatedPath finds a free name for the file being set aside. A second is a
// long time once a line-count trigger is in play, so the stamp alone is not
// enough: two rotations in the same second would rename the first one's
// contents into oblivion.
func (s *FileSink) rotatedPath(now time.Time) string {
	base := filepath.Join(s.dir, fmt.Sprintf("mcpx-%s-%s", s.day, now.Format("150405")))
	if _, err := os.Stat(base + ".jsonl"); os.IsNotExist(err) {
		return base + ".jsonl"
	}
	for n := 1; ; n++ {
		p := fmt.Sprintf("%s.%d.jsonl", base, n)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
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
