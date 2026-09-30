package cli

import (
	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/settings"
)

// fileOptions builds the durable log's rotation policy from the resolved
// settings.
//
// The four knobs -- size, lines, age, retention -- were declared, documented
// and never passed anywhere, so every sink in the program ran on the built-in
// defaults and a user who set logging.maxBytes watched nothing change. They
// are read here, in one place, so the daemon's log and a script's log rotate
// by the same rule; a run whose records land in a file with a different
// retention from the daemon's is a log nobody can reason about.
func fileOptions(set *settings.Set, dir string) logging.FileOptions {
	return logging.FileOptions{
		Dir:      dir,
		MaxBytes: set.Bytes("logging.maxBytes"),
		MaxLines: int64(set.Int("logging.maxLines")),
		MaxAge:   set.Duration("logging.maxAge"),
		Keep:     set.Int("logging.keep"),
	}
}
