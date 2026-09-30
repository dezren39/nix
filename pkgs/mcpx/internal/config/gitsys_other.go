//go:build !unix

package config

import "os"

// Without a device number there is no filesystem boundary to stop at, which
// is what GIT_DISCOVERY_ACROSS_FILESYSTEM=1 means anyway.
var gitDevice = func(string) (uint64, error) { return 0, nil }

func accessX(path string) bool { _, err := os.Stat(path); return err == nil }
