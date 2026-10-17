// Package secrets holds the values a person gives mcpx for the secrets its
// servers require, and the requests that ask for them.
//
// Values live in memory and nowhere else. Nothing in this package touches the
// disk, and no method renders a value as text: a response, a log line or an
// error cannot carry one by accident. The only consumer of a value is the
// environment of the child process that requires it. Names are what leave.
package secrets

import (
	"errors"
	"regexp"
	"sort"
	"sync"
)

// nameOK is what can be an environment variable name, which is how a secret
// reaches the process that needs it.
var nameOK = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidName reports whether name can be given a value.
func ValidName(name string) bool { return nameOK.MatchString(name) }

// Store holds runtime values by name.
type Store struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{values: map[string]string{}} }

// String keeps a store out of %v output. It holds values, so it says nothing
// about what it holds. GoString does the same for %#v, which would otherwise
// print the map the values are kept in.
func (s *Store) String() string   { return "secrets.Store{values redacted}" }
func (s *Store) GoString() string { return "secrets.Store{values redacted}" }

// Set stores value for name, replacing any value already held. An empty value
// is refused rather than stored, because an empty secret reads as absent to
// everything that checks for one.
func (s *Store) Set(name, value string) error {
	if value == "" {
		return errors.New("a secret needs a value; an empty one is not stored")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[name] = value
	return nil
}

// Unset drops the value held for name and reports whether there was one.
func (s *Store) Unset(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.values[name]
	delete(s.values, name)
	return ok
}

// Lookup returns the value held for name. It exists to put the value into a
// child's environment, and is not for anything a person or a caller reads.
func (s *Store) Lookup(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[name]
	return v, ok
}

// Names returns the names that have a value, sorted.
func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.values))
	for name := range s.values {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
