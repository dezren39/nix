package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dezren39/mcpx/internal/defaults"
)

// ExecIdentity says which program a stdio server's command runs, as of now.
//
// PoolID describes the configuration; it cannot see a binary that changed
// underneath an unchanged command. `npx`, `uvx` and a nix profile that
// re-points `~/.nix-profile/bin/foo` all keep the same command, args and env
// while the program behind them moves, so a pool keyed by PoolID alone kept
// running the old program until somebody restarted the daemon.
//
// The identity is what the file holds, not where it is: its content hash when
// it is small, and its device, inode, size, mtime and mode when it is not. A
// symlink re-pointed at a byte-identical file is the same program and keeps
// its child. A re-pointed link to a different file is a different program,
// whatever its path, because its bytes or its inode differ. The stat fallback
// keeps a large program, such as a node binary, from being read whole on every
// check. An in-place rewrite changes size or mtime in practice; a replacement
// changes the inode.
//
// Caveat: the bytes of the file are the whole identity, so a program that finds
// its own files relative to its real location is not. A node script beside its
// node_modules, re-pointed at an identical copy in another directory, keeps its
// child, which still runs with the dependencies of the old location. A restart
// (mcpx restart) is what moves it. Above the hash limit the identity is the
// inode, so such a copy is restarted instead.
//
// The identity is "" for a server that is not a child process, and begins
// with "missing:" when the command does not resolve. A missing command is
// its own identity, so the pool still starts and fails the way it always did,
// and it is replaced when the command appears.
func (r *Resolved) ExecIdentity() string {
	_, id, _ := r.ExecState()
	return id
}

// ExecState is the stamp of the executable a command resolves to, and its
// identity. ok is false when the command does not resolve or the file cannot
// be read, in which case identity is the "missing:" form ExecIdentity gives.
//
// The stamp is taken before the identity is worked out, so a rewrite that
// lands in between leaves a stamp that differs from the file and the next
// check sees it.
func (r *Resolved) ExecState() (st ExecStamp, identity string, ok bool) {
	if !r.Stdio() {
		return ExecStamp{}, "", false
	}
	st, found := r.ExecStamp()
	if found {
		if id, err := st.Identity(); err == nil {
			return st, id, true
		}
	}
	return ExecStamp{}, "missing:" + r.Command, false
}

// ExecStamp is the cheap part of an executable's identity: what a stat says
// about the file the command resolves to. A periodic check compares stamps and
// only works out an identity when one moved.
type ExecStamp struct {
	// Path is the resolved real path, after symlinks.
	Path  string
	Inode string
	Size  int64
	MTime int64
	Mode  fs.FileMode
}

// ExecStamp stats the file the command resolves to. It reports false for a
// server that is not a child process, and for a command that does not
// resolve to a file it can stat.
func (r *Resolved) ExecStamp() (ExecStamp, bool) {
	if !r.Stdio() {
		return ExecStamp{}, false
	}
	path := r.resolveExec()
	if path == "" {
		return ExecStamp{}, false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ExecStamp{}, false
	}
	fi, err := os.Stat(real)
	if err != nil {
		return ExecStamp{}, false
	}
	// Not Sys(): its atime moves every time the program runs, which would
	// count as a change on every check.
	return ExecStamp{
		Path:  real,
		Inode: fileInode(fi),
		Size:  fi.Size(),
		MTime: fi.ModTime().UnixNano(),
		Mode:  fi.Mode(),
	}, true
}

// Identity works out what the stamped file holds. It reads the whole file only
// when the file is small enough to hash, and an error means it could not be
// read, so the caller treats it as missing.
func (s ExecStamp) Identity() (string, error) {
	h := sha256.New()
	if s.Mode.IsRegular() && s.Size <= defaults.ExecHashMax {
		f, err := os.Open(s.Path)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		return "sha256:" + hex.EncodeToString(h.Sum(nil))[:16], nil
	}
	fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d", s.Inode, s.Size, s.MTime, s.Mode)
	return "stat:" + hex.EncodeToString(h.Sum(nil))[:16], nil
}

// ProcessID is the identity two servers must share to share a pool: the
// configured process definition and the executable it currently resolves to.
func (r *Resolved) ProcessID() string {
	return r.PoolID() + "/" + r.ExecIdentity()
}

// resolveExec finds the file the child will run. It searches the PATH the
// child will see -- the server's own env if it sets one, else the daemon's --
// rather than the daemon's PATH alone, since a server can set its own.
func (r *Resolved) resolveExec() string {
	if strings.Contains(r.Command, string(filepath.Separator)) {
		p := r.Command
		if !filepath.IsAbs(p) && r.Cwd != "" {
			p = filepath.Join(r.Cwd, p)
		}
		if isExecutable(p) {
			return p
		}
		return ""
	}
	pathEnv, ok := r.Env["PATH"]
	if !ok {
		pathEnv = os.Getenv("PATH")
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		p := filepath.Join(dir, r.Command)
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode().Perm()&0o111 != 0
}
