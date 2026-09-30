package config

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// This file reads the part of a repository's config that decides where its
// worktree is: core.repositoryformatversion, core.bare, core.worktree and the
// extensions.* table. It is a port of git's own parser (config.c:
// git_parse_source and friends) and of setup.c:check_repo_format, because
// "close enough" is how two programs end up disagreeing about which
// repository a directory is in.
//
// It does not follow [include] or [includeIf]. Neither does git here: the
// repository format is read with git_config_from_file, which has no include
// handling, and a core.worktree set through an include does not move
// --show-toplevel. The parity tests pin that.

// errConfigSyntax is a config file git itself would refuse ("bad config
// line"). It is reported as unsure rather than as a failure, so the fallback
// gets the final word; a newer git may accept what this parser does not.
var errConfigSyntax = errors.New("config syntax")

// cfgReader reproduces git's get_next_char: CRLF reads as LF, and end of
// input reads as one final LF with eof set.
type cfgReader struct {
	b   []byte
	i   int
	eof bool
}

func (r *cfgReader) next() byte {
	if r.i >= len(r.b) {
		r.eof = true
		return '\n'
	}
	c := r.b[r.i]
	r.i++
	if c == '\r' && r.i < len(r.b) && r.b[r.i] == '\n' {
		r.i++
		return '\n'
	}
	return c
}

// Git's sane_ctype, not C's locale-dependent one: \v and \f are not space.
func gitSpace(c byte) bool   { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func gitAlpha(c byte) bool   { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }
func gitKeyChar(c byte) bool { return gitAlpha(c) || (c >= '0' && c <= '9') || c == '-' }
func gitLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// parseGitConfig calls fn for every variable in b with its full name as git
// spells it internally -- section and key lower-cased, subsection verbatim --
// and its value, nil for a key written without "=". It stops at the first
// error, as git does.
func parseGitConfig(b []byte, fn func(name string, value *string) error) error {
	r := &cfgReader{b: b}
	// A UTF-8 byte order mark is skipped; a partial one is an error.
	if len(b) > 0 && b[0] == 0xEF {
		if len(b) < 3 || b[1] != 0xBB || b[2] != 0xBF {
			return errConfigSyntax
		}
		r.i = 3
	}
	section := ""
	comment := false
	for {
		c := r.next()
		switch {
		case c == '\n':
			if r.eof {
				return nil
			}
			comment = false
			continue
		case comment:
			continue
		case gitSpace(c):
			continue
		case c == '#' || c == ';':
			comment = true
			continue
		case c == '[':
			name, err := r.baseVar()
			if err != nil || name == "" {
				return errConfigSyntax
			}
			section = name + "."
			continue
		case !gitAlpha(c):
			return errConfigSyntax
		}
		key, value, err := r.keyValue(c)
		if err != nil {
			return err
		}
		if err := fn(section+key, value); err != nil {
			return err
		}
	}
}

func (r *cfgReader) baseVar() (string, error) {
	var name []byte
	for {
		c := r.next()
		if r.eof {
			return "", errConfigSyntax
		}
		if c == ']' {
			return string(name), nil
		}
		if gitSpace(c) {
			return r.extendedBaseVar(name, c)
		}
		if !gitKeyChar(c) && c != '.' {
			return "", errConfigSyntax
		}
		name = append(name, gitLower(c))
	}
}

// extendedBaseVar reads the `"subsection"]` of `[section "subsection"]`.
// The subsection keeps its case and takes backslash escapes literally.
func (r *cfgReader) extendedBaseVar(name []byte, c byte) (string, error) {
	for {
		if c == '\n' {
			return "", errConfigSyntax
		}
		c = r.next()
		if !gitSpace(c) {
			break
		}
	}
	if c != '"' {
		return "", errConfigSyntax
	}
	name = append(name, '.')
	for {
		c := r.next()
		if c == '\n' {
			return "", errConfigSyntax
		}
		if c == '"' {
			break
		}
		if c == '\\' {
			if c = r.next(); c == '\n' {
				return "", errConfigSyntax
			}
		}
		name = append(name, c)
	}
	if r.next() != ']' {
		return "", errConfigSyntax
	}
	return string(name), nil
}

func (r *cfgReader) keyValue(first byte) (string, *string, error) {
	key := []byte{gitLower(first)}
	var c byte
	for {
		c = r.next()
		if r.eof || !gitKeyChar(c) {
			break
		}
		key = append(key, gitLower(c))
	}
	for c == ' ' || c == '\t' {
		c = r.next()
	}
	if c == '\n' {
		return string(key), nil, nil
	}
	// Anything but "=" after a key is an error in git, including a comment
	// straight after a bare key.
	if c != '=' {
		return "", nil, errConfigSyntax
	}
	v, err := r.value()
	if err != nil {
		return "", nil, err
	}
	return string(key), &v, nil
}

// value is git's parse_value, including its trailing-space bookkeeping:
// trim is zero while unset, so leading spaces are dropped, interior ones
// kept, and trailing unquoted ones removed.
func (r *cfgReader) value() (string, error) {
	var buf []byte
	quote, comment := false, false
	trim := 0
	for {
		c := r.next()
		if c == '\n' {
			if quote {
				return "", errConfigSyntax
			}
			if trim != 0 {
				buf = buf[:trim]
			}
			return string(buf), nil
		}
		if comment {
			continue
		}
		if gitSpace(c) && !quote {
			if trim == 0 {
				trim = len(buf)
			}
			if len(buf) > 0 {
				buf = append(buf, c)
			}
			continue
		}
		if !quote && (c == ';' || c == '#') {
			comment = true
			continue
		}
		trim = 0
		if c == '\\' {
			switch c = r.next(); c {
			case '\n':
				continue
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'n':
				c = '\n'
			case '\\', '"':
			default:
				return "", errConfigSyntax
			}
			buf = append(buf, c)
			continue
		}
		if c == '"' {
			quote = !quote
			continue
		}
		buf = append(buf, c)
	}
}

// repoFormat is what setup.c:read_repository_format collects.
type repoFormat struct {
	version        int // -1 when the key is absent
	bare           int // -1 unset, else 0 or 1
	worktree       *string
	worktreeConfig bool
	v1Only         []string
	unknown        []string
}

// unsureValue is a value git would refuse, or one this port does not parse
// the way git does. Either way git decides.
func unsureValue(name string, value *string) error {
	if value == nil {
		return fmt.Errorf("%s has no value", name)
	}
	return fmt.Errorf("%s = %q is not a value mcpx reads", name, *value)
}

// gitBool accepts what git_parse_maybe_bool_text does, and plain decimal
// integers. Git also takes k/m/g suffixes, hex and octal here; nobody writes
// those for a boolean, and refusing them only means asking git.
func gitBool(name string, value *string) (bool, error) {
	if value == nil {
		return true, nil
	}
	switch strings.ToLower(*value) {
	case "":
		return false, nil
	case "true", "yes", "on":
		return true, nil
	case "false", "no", "off":
		return false, nil
	}
	n, err := strconv.Atoi(*value)
	if err != nil {
		return false, unsureValue(name, value)
	}
	return n != 0, nil
}

// set is check_repo_format for one variable.
func (f *repoFormat) set(name string, value *string) error {
	if name == "core.repositoryformatversion" {
		if value == nil {
			return unsureValue(name, value)
		}
		n, err := strconv.Atoi(*value)
		if err != nil {
			return unsureValue(name, value)
		}
		f.version = n
		return nil
	}
	if ext, ok := strings.CutPrefix(name, "extensions."); ok {
		return f.extension(name, ext, value)
	}
	return f.setWorktree(name, value)
}

// extension mirrors handle_extension_v0 and handle_extension. The first group
// is honoured in any repository version; the second only from version 1, and
// a version-0 repository that names one is refused.
func (f *repoFormat) extension(name, ext string, value *string) error {
	switch ext {
	case "noop":
		return nil
	case "preciousobjects":
		_, err := gitBool(name, value)
		return err
	case "partialclone":
		if value == nil {
			return unsureValue(name, value)
		}
		return nil
	case "worktreeconfig":
		b, err := gitBool(name, value)
		f.worktreeConfig = b
		return err
	case "noop-v1":
	case "objectformat", "compatobjectformat":
		if value == nil || (*value != "sha1" && *value != "sha256") {
			return unsureValue(name, value)
		}
		if ext == "compatobjectformat" && slices.Contains(f.v1Only, ext) {
			return unsureValue(name, value)
		}
	case "refstorage":
		if value == nil {
			return unsureValue(name, value)
		}
		// A reference backend may carry a payload: "reftable://path".
		backend, _, _ := strings.Cut(*value, "://")
		if backend != "files" && backend != "reftable" {
			return unsureValue(name, value)
		}
	case "relativeworktrees", "submodulepathconfig":
		if _, err := gitBool(name, value); err != nil {
			return err
		}
	default:
		f.unknown = append(f.unknown, ext)
		return nil
	}
	f.v1Only = append(f.v1Only, ext)
	return nil
}

// setWorktree is read_worktree_config: the two keys that move or remove the
// worktree, and the only two config.worktree is consulted for.
func (f *repoFormat) setWorktree(name string, value *string) error {
	switch name {
	case "core.bare":
		b, err := gitBool(name, value)
		if err != nil {
			return err
		}
		f.bare = 0
		if b {
			f.bare = 1
		}
	case "core.worktree":
		if value == nil {
			return unsureValue(name, value)
		}
		v := *value
		f.worktree = &v
	}
	return nil
}

// verify is verify_repository_format. Every refusal is returned as unsure:
// the git on this machine may be newer than this port and know the
// extension or version.
func (f *repoFormat) verify() error {
	switch {
	case f.version > 1:
		return fmt.Errorf("repository format version %d is newer than mcpx reads", f.version)
	case f.version >= 1 && len(f.unknown) > 0:
		return fmt.Errorf("unknown repository extension %s", strings.Join(f.unknown, ", "))
	case f.version == 0 && len(f.v1Only) > 0:
		return fmt.Errorf("version-0 repository uses %s, which needs version 1", strings.Join(f.v1Only, ", "))
	}
	return nil
}
