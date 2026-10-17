//go:build !unix

package config

import "os"

// fileInode has no portable form off unix. Size, mtime and mode still tell
// a rewritten file apart; a replacement with the same size and mtime is missed
// there, which is the residual gap the exec watch documents.
func fileInode(fi os.FileInfo) string { return "" }
