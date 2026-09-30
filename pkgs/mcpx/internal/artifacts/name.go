package artifacts

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/dezren39/mcpx/internal/defaults"
)

// SanitizeName makes a script-supplied name safe to write to disk.
//
// The name comes from the script, and a script is not trusted with a path.
// The three ways a name escapes a directory are an absolute path, a ..
// component, and a separator that turns one name into a nested path; all
// three are removed rather than rejected, because a caller whose artifact is
// refused over its name has lost the file, and the name is decoration.
//
// Everything outside a conservative set becomes an underscore. That is wider
// than strictly necessary on a modern filesystem and narrower than what a
// shell will mishandle, and the file is reached by id anyway -- the name only
// has to be recognisable.
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	// Take the last component: "a/b/../c.png" is a path, and the file is
	// called c.png.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_' || r == '+':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	// "..", "." and "" all collapse to nothing useful above, and a file has
	// to be called something.
	if out == "" {
		return "artifact"
	}
	// A leading dot would hide the file from an ordinary listing, which is
	// not what a caller asking for `.env` in an output directory expects to
	// have to remember.
	if len(out) > defaults.ArtifactNameMaxLength {
		ext := filepath.Ext(out)
		if len(ext) > defaults.ArtifactNameMaxLength/2 {
			ext = ""
		}
		out = out[:defaults.ArtifactNameMaxLength-len(ext)] + ext
	}
	return out
}

// MimeForName guesses a content type from an extension.
//
// A guess, and stated as one: the script may pass the type explicitly and
// that always wins. The fallback is the universal "some bytes" type rather
// than text, because treating an unknown binary as text is how a terminal
// gets filled with control characters.
func MimeForName(name string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		return t
	}
	return "application/octet-stream"
}

// uniquePath resolves a name inside a directory without ever overwriting.
//
// The suffix goes before the extension -- shot-1.png, not shot.png-1 -- so
// the file still opens in whatever the extension implies.
func uniquePath(dir, name string) (string, error) {
	name = SanitizeName(name)
	// Resolved so that a symlinked output directory is still written inside
	// itself. Checked after joining, because the check has to be on the path
	// that will actually be opened.
	base := filepath.Clean(dir)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < defaults.ArtifactNameCollisionLimit; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		p := filepath.Join(base, candidate)
		if !strings.HasPrefix(p, base+string(filepath.Separator)) {
			return "", fmt.Errorf("artifact name %q does not stay inside %s", name, dir)
		}
		// O_EXCL is the check and the claim in one step. Testing with Stat
		// and then creating is a race two concurrent runs of the same script
		// would lose.
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			// Removed again so Link can have the name. The window is the
			// price of reserving it at all; without the reservation two
			// concurrent exports pick the same name and one is lost.
			_ = os.Remove(p)
			return p, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("%s already holds %d files called %q", dir, defaults.ArtifactNameCollisionLimit, name)
}
