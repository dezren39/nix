//go:build unix

package config

import (
	"fmt"
	"os"
	"syscall"
)

// fileInode is the device and inode of a file, which a replacement changes
// even when its size and mtime happen to match.
func fileInode(fi os.FileInfo) string {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
	}
	return ""
}
