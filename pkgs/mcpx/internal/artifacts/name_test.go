package artifacts_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/artifacts"
)

func asErr(err error, target any) bool { return errors.As(err, target) }

func sameInode(t *testing.T, a, b string) bool {
	t.Helper()
	sa, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(sa, sb)
}

// Names come from a script, and a script is not trusted with a path.
func TestSanitizeNameCannotEscapeADirectory(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"shot.png", "shot.png"},
		{"../../etc/passwd", "passwd"},
		{"/etc/passwd", "passwd"},
		{"a/b/c.txt", "c.txt"},
		{`..\..\windows\system32`, "system32"},
		{"..", "artifact"},
		{".", "artifact"},
		{"", "artifact"},
		{"   ", "artifact"},
		{"my shot.png", "my-shot.png"},
		{"weird;|&$name", "weird____name"},
	} {
		if got := artifacts.SanitizeName(tc.in); got != tc.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizedNamesStayInsideTheTargetDirectory(t *testing.T) {
	dir := t.TempDir()
	s, err := artifacts.Open(artifacts.Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m, err := s.Put(strings.NewReader("x"), artifacts.PutOptions{Name: "../../escape.txt"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Export(m, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != dir {
		t.Errorf("wrote outside %s: %s", dir, p)
	}
}

func TestALongNameIsTruncatedButKeepsItsExtension(t *testing.T) {
	long := strings.Repeat("a", 400) + ".png"
	got := artifacts.SanitizeName(long)
	if len(got) > 255 {
		t.Errorf("name is %d bytes, past the component limit", len(got))
	}
	if !strings.HasSuffix(got, ".png") {
		t.Errorf("the extension should survive: %q", got[len(got)-10:])
	}
}

func TestURIRoundTrips(t *testing.T) {
	id := artifacts.NewID()
	back, ok := artifacts.IDFromURI(artifacts.URI(id))
	if !ok || back != id {
		t.Errorf("round trip failed: %q -> %q (%v)", id, back, ok)
	}
	// A namespaced resource must not be mistaken for an artifact, or reading
	// one would look up the wrong thing entirely.
	if _, ok := artifacts.IDFromURI("mcpx://fff/search"); ok {
		t.Error("a server resource should not parse as an artifact")
	}
	if _, ok := artifacts.IDFromURI("mcpx://artifacts/a/b"); ok {
		t.Error("an id with a separator should be refused")
	}
}

func TestMimeIsGuessedFromTheExtension(t *testing.T) {
	if got := artifacts.MimeForName("a.png"); got != "image/png" {
		t.Errorf("a.png -> %q", got)
	}
	// Unknown is "some bytes", not text: rendering an unknown binary as text
	// is how a terminal fills with control characters.
	if got := artifacts.MimeForName("a.qqq"); got != "application/octet-stream" {
		t.Errorf("a.qqq -> %q", got)
	}
}
