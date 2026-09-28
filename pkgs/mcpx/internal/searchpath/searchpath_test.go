package searchpath_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/searchpath"
)

const null = "\x00null"

func TestAnEmptyListMeansTheBuiltInPath(t *testing.T) {
	got := searchpath.Resolve(nil, null, searchpath.Options{
		Dir: "/w", Builtin: []string{"/a", "/b"},
	})
	if len(got.Entries) != 2 || got.Entries[0].Path != "/a" {
		t.Errorf("got %+v", got.Entries)
	}
}

func TestAListWithoutNullReplacesTheBuiltIn(t *testing.T) {
	got := searchpath.Resolve([]string{"/only"}, null, searchpath.Options{
		Dir: "/w", Builtin: []string{"/a"},
	})
	if len(got.Entries) != 1 || got.Entries[0].Path != "/only" {
		t.Errorf("got %+v", got.Entries)
	}
}

func TestNullStandsForWhateverWasAlreadyThere(t *testing.T) {
	// The point: saying "mine first, then the usual places" without having
	// to restate the usual places.
	got := searchpath.Resolve([]string{"/mine", null, "/last"}, null, searchpath.Options{
		Dir: "/w", Builtin: []string{"/a", "/b"},
	})
	var paths []string
	for _, e := range got.Entries {
		paths = append(paths, e.Path)
	}
	if strings.Join(paths, ",") != "/mine,/a,/b,/last" {
		t.Errorf("got %v", paths)
	}
}

func TestRelativeEntriesResolveAgainstTheGivenDirectory(t *testing.T) {
	got := searchpath.Resolve([]string{"./mine"}, null, searchpath.Options{Dir: "/work"})
	if got.Entries[0].Path != "/work/mine" {
		t.Errorf("got %q", got.Entries[0].Path)
	}
}

func TestDuplicatesCollapseKeepingTheFirst(t *testing.T) {
	got := searchpath.Resolve([]string{"/a", "/b", "/a"}, null, searchpath.Options{Dir: "/w"})
	if len(got.Entries) != 2 {
		t.Errorf("got %+v", got.Entries)
	}
}

func TestAnEntryMayNameAFileRatherThanADirectory(t *testing.T) {
	// Someone with one script in an odd place should be able to name it
	// without inventing a directory to hold it.
	dir := t.TempDir()
	file := filepath.Join(dir, "lonely.ts")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := searchpath.Resolve([]string{file}, null, searchpath.Options{Dir: dir})
	if !got.Entries[0].IsFile {
		t.Fatalf("should be recognised as a file: %+v", got.Entries[0])
	}
	found, _, err := got.Find("lonely", searchpath.FindOptions{Extensions: []string{".ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if found != file {
		t.Errorf("a file entry should match by stem: got %q", found)
	}
}

func TestFindPrefersTheNearerEntry(t *testing.T) {
	near, far := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(near, "dup.ts"), []byte("NEAR"), 0o644)
	_ = os.WriteFile(filepath.Join(far, "dup.ts"), []byte("FAR"), 0o644)

	got := searchpath.Resolve([]string{near, far}, null, searchpath.Options{Dir: near})
	found, _, err := got.Find("dup", searchpath.FindOptions{Extensions: []string{".ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(found, near) {
		t.Errorf("the nearer entry should win: %q", found)
	}
}

func TestATypescriptAndJavascriptPairIsRefusedByDefault(t *testing.T) {
	// Which one runs is a coin flip nobody should have to call, and silently
	// preferring one means an edit to the other does nothing with no
	// indication why.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "both.ts"), []byte("ts"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "both.js"), []byte("js"), 0o644)

	got := searchpath.Resolve([]string{dir}, null, searchpath.Options{Dir: dir})
	_, amb, err := got.Find("both", searchpath.FindOptions{Extensions: []string{".ts", ".js"}})
	if err == nil {
		t.Fatal("an ambiguous pair should be refused")
	}
	if amb == nil || len(amb.Paths) != 2 {
		t.Errorf("both candidates should be named: %+v", amb)
	}
	if !strings.Contains(err.Error(), "allowTsJsOverlap") {
		t.Errorf("the error should name the switch that changes it: %v", err)
	}
}

func TestOverlapCanBeAllowedAndThenTypescriptWins(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "both.ts"), []byte("ts"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "both.js"), []byte("js"), 0o644)

	got := searchpath.Resolve([]string{dir}, null, searchpath.Options{Dir: dir})
	found, _, err := got.Find("both", searchpath.FindOptions{
		Extensions: []string{".ts", ".js"}, AllowOverlap: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(found, ".ts") {
		t.Errorf("the extension list is ordered deliberately; .ts should win: %q", found)
	}
}

func TestListReportsEveryNameNearestWinning(t *testing.T) {
	near, far := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(near, "a.ts"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(near, "shared.ts"), []byte("near"), 0o644)
	_ = os.WriteFile(filepath.Join(far, "b.ts"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(far, "shared.ts"), []byte("far"), 0o644)

	got := searchpath.Resolve([]string{near, far}, null, searchpath.Options{Dir: near})
	names, amb := got.List(searchpath.FindOptions{Extensions: []string{".ts"}})
	if len(amb) != 0 {
		t.Errorf("no ambiguity expected: %v", amb)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"a", "b", "shared"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q missing from %q", want, joined)
		}
	}
	if strings.Count(joined, "shared") != 1 {
		t.Errorf("shared should appear once: %q", joined)
	}
}

func TestDescribeMarksWhatIsMissingAndWhatIsAFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.ts")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	got := searchpath.Resolve([]string{dir, file, "/definitely/not/here"}, null,
		searchpath.Options{Dir: dir})
	desc := got.Describe()
	if !strings.Contains(desc, "- /definitely/not/here") {
		t.Errorf("a missing entry should be marked:\n%s", desc)
	}
	if !strings.Contains(desc, "f "+file) {
		t.Errorf("a file entry should be marked:\n%s", desc)
	}
}

func TestMissingEntriesAreKeptUnlessAskedOtherwise(t *testing.T) {
	// A missing directory is not an error, it is simply empty; reporting it
	// as one would make every fresh checkout noisy.
	got := searchpath.Resolve([]string{"/nope"}, null, searchpath.Options{Dir: "/w"})
	if len(got.Entries) != 1 {
		t.Errorf("kept by default: %+v", got.Entries)
	}
	pruned := searchpath.Resolve([]string{"/nope"}, null, searchpath.Options{
		Dir: "/w", RequireExists: true,
	})
	if len(pruned.Entries) != 0 {
		t.Errorf("dropped when asked: %+v", pruned.Entries)
	}
}
