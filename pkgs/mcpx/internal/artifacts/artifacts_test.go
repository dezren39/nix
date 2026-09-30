package artifacts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/artifacts"
)

func open(t *testing.T, opts artifacts.Options) *artifacts.Store {
	t.Helper()
	if opts.Dir == "" {
		opts.Dir = t.TempDir()
	}
	if opts.TTL == 0 {
		opts.TTL = time.Hour
	}
	s, err := artifacts.Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPutStoresAndReadsBack(t *testing.T) {
	s := open(t, artifacts.Options{})
	m, err := s.Put(strings.NewReader("hello"), artifacts.PutOptions{Name: "greeting.txt", Run: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Size != 5 {
		t.Errorf("size = %d", m.Size)
	}
	if !strings.HasPrefix(m.ID, "art-") {
		t.Errorf("id should be a handle, got %q", m.ID)
	}
	if m.URI != "mcpx://artifacts/"+m.ID {
		t.Errorf("uri = %q", m.URI)
	}
	if m.Mime != "text/plain; charset=utf-8" {
		t.Errorf("the extension should decide the type, got %q", m.Mime)
	}
	body, _, err := s.Bytes(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q", body)
	}
}

// The handle must not be the hash. A content hash is guessable by anyone who
// can guess the content, and the handle is the whole of the access control
// until OAuth scopes land.
func TestTheHandleIsNotTheContentHash(t *testing.T) {
	s := open(t, artifacts.Options{})
	m, err := s.Put(strings.NewReader("hello"), artifacts.PutOptions{Name: "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.ID, m.SHA256) || strings.Contains(m.SHA256, strings.TrimPrefix(m.ID, "art-")) {
		t.Errorf("the id %q leaks the hash %q", m.ID, m.SHA256)
	}
	second, err := s.Put(strings.NewReader("hello"), artifacts.PutOptions{Name: "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == m.ID {
		t.Error("two registrations of the same bytes should get different handles")
	}
	if second.SHA256 != m.SHA256 {
		t.Error("the same bytes should share one content address")
	}
}

func TestIdenticalContentIsStoredOnce(t *testing.T) {
	dir := t.TempDir()
	s := open(t, artifacts.Options{Dir: dir})
	for i := 0; i < 3; i++ {
		if _, err := s.Put(strings.NewReader("same"), artifacts.PutOptions{Name: "x.bin"}); err != nil {
			t.Fatal(err)
		}
	}
	var blobs int
	_ = filepath.Walk(filepath.Join(dir, "blobs"), func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			blobs++
		}
		return nil
	})
	if blobs != 1 {
		t.Errorf("three registrations of one body should keep one blob, found %d", blobs)
	}
	used, err := s.Used()
	if err != nil {
		t.Fatal(err)
	}
	if used != 4 {
		t.Errorf("usage should count distinct content, got %d", used)
	}
}

func TestPerArtifactLimitIsAnErrorNotATruncation(t *testing.T) {
	s := open(t, artifacts.Options{MaxBytes: 4})
	_, err := s.Put(strings.NewReader("much too long"), artifacts.PutOptions{Name: "big"})
	var tooLarge artifacts.ErrTooLarge
	if err == nil {
		t.Fatal("over the limit should fail")
	}
	if !asErr(err, &tooLarge) {
		t.Fatalf("want ErrTooLarge, got %T: %v", err, err)
	}
	list, _ := s.List(artifacts.Filter{})
	if len(list) != 0 {
		t.Errorf("a refused artifact should not be registered: %+v", list)
	}
}

func TestQuotaRefusesNewContentButNotDuplicates(t *testing.T) {
	s := open(t, artifacts.Options{Quota: 10})
	if _, err := s.Put(strings.NewReader("0123456789"), artifacts.PutOptions{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	// The same bytes again free no space by being refused, so they are not.
	if _, err := s.Put(strings.NewReader("0123456789"), artifacts.PutOptions{Name: "b"}); err != nil {
		t.Errorf("a duplicate should not hit the quota: %v", err)
	}
	_, err := s.Put(strings.NewReader("x"), artifacts.PutOptions{Name: "c"})
	var quota artifacts.ErrQuota
	if !asErr(err, &quota) {
		t.Fatalf("want ErrQuota, got %v", err)
	}
}

func TestPutFileHardlinksRatherThanCopies(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(src, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := open(t, artifacts.Options{})
	m, err := s.PutFile(src, artifacts.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "shot.png" {
		t.Errorf("the name should default to the file's, got %q", m.Name)
	}
	if !sameInode(t, src, s.BodyPath(m)) {
		t.Error("a file on the same filesystem should be linked, not copied")
	}
}

func TestExportNeverOverwrites(t *testing.T) {
	s := open(t, artifacts.Options{})
	out := t.TempDir()
	var paths []string
	for _, body := range []string{"one", "two", "three"} {
		m, err := s.Put(strings.NewReader(body), artifacts.PutOptions{Name: "shot.png"})
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.Export(m, out)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	seen := map[string]bool{}
	for i, p := range paths {
		if seen[p] {
			t.Fatalf("export %d reused %s; a second file called shot.png is a second file", i, p)
		}
		seen[p] = true
		if filepath.Ext(p) != ".png" {
			t.Errorf("the suffix should go before the extension, got %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "shot-1.png")); err != nil {
		t.Errorf("expected shot-1.png: %v", err)
	}
}

func TestExpiredArtifactsAreCollected(t *testing.T) {
	s := open(t, artifacts.Options{TTL: -time.Second})
	if _, err := s.Put(strings.NewReader("gone soon"), artifacts.PutOptions{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.GC(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("collected %d", n)
	}
	list, _ := s.List(artifacts.Filter{})
	if len(list) != 0 {
		t.Errorf("still listed: %+v", list)
	}
}

func TestDeleteKeepsABodyAnotherRegistrationShares(t *testing.T) {
	s := open(t, artifacts.Options{})
	a, _ := s.Put(strings.NewReader("shared"), artifacts.PutOptions{Name: "a"})
	b, _ := s.Put(strings.NewReader("shared"), artifacts.PutOptions{Name: "b"})
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Bytes(b.ID); err != nil {
		t.Fatalf("the surviving registration lost its body: %v", err)
	}
	if err := s.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.BodyPath(b)); !os.IsNotExist(err) {
		t.Error("the last registration should take the body with it")
	}
}

func TestListFiltersByRunOldestLast(t *testing.T) {
	s := open(t, artifacts.Options{})
	for _, r := range []string{"r1", "r2", "r1"} {
		if _, err := s.Put(strings.NewReader(r+"x"), artifacts.PutOptions{Name: "n", Run: r}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(artifacts.Filter{Run: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 for r1, got %d", len(got))
	}
	for _, m := range got {
		if m.Run != "r1" {
			t.Errorf("leaked %s", m.Run)
		}
	}
}

func TestMissingArtifactSaysItMayHaveExpired(t *testing.T) {
	s := open(t, artifacts.Options{})
	_, err := s.Get("art-nope")
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("want an expiry hint, got %v", err)
	}
}
