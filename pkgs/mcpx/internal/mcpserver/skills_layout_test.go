package mcpserver

import (
	"reflect"
	"testing"
	"testing/fstest"
)

// resources/directory/read over a skill layout the shipped skills do not
// have: nested subfolders, an empty folder, a binary file, a nested
// SKILL.md. Users put anything in a skill folder, so the listing must follow
// the files rather than any one shape.
func TestDirectoryReadFollowsAnyLayout(t *testing.T) {
	fsys := fstest.MapFS{
		"demo/SKILL.md":                {Data: []byte("---\nname: demo\ndescription: d\n---\n")},
		"demo/notes.md":                {Data: []byte("n")},
		"demo/a/b/c/deep.ts":           {Data: []byte("x")},
		"demo/a/top.bin":               {Data: []byte{0xff, 0xfe}},
		"demo/empty":                   {Mode: 0o755 | 1<<31}, // fs.ModeDir
		"demo/inner/SKILL.md":          {Data: []byte("---\nname: inner\ndescription: i\n---\n")},
		"other/SKILL.md":               {Data: []byte("---\nname: other\ndescription: o\n---\n")},
		"other/examples/one.ts":        {Data: []byte("1")},
		"other/examples/nested/two.ts": {Data: []byte("2")},
	}
	ks, err := loadSkills(fsys)
	if err != nil {
		t.Fatal(err)
	}
	const root = "skill://mcpx/demo"
	// Each child, true when it is listed as a directory resource.
	list := func(dir string) map[string]bool {
		t.Helper()
		got, ok := dirChildren(ks, dir)
		if !ok {
			t.Fatalf("%s: not a directory", dir)
		}
		out := map[string]bool{}
		for _, r := range got {
			out[r.URI] = r.MimeType == dirMime
			if r.MimeType == "" {
				t.Errorf("%s has no mimeType", r.URI)
			}
		}
		return out
	}
	const other = "skill://mcpx/other/examples"
	cases := map[string]map[string]bool{
		root: {root + "/SKILL.md": false, root + "/notes.md": false,
			root + "/a": true, root + "/empty": true, root + "/inner": true},
		root + "/a":     {root + "/a/b": true, root + "/a/top.bin": false},
		root + "/a/b":   {root + "/a/b/c": true},
		root + "/a/b/c": {root + "/a/b/c/deep.ts": false},
		root + "/empty": {},
		// A nested SKILL.md is an ordinary supporting file of the enclosing skill.
		root + "/inner":   {root + "/inner/SKILL.md": false},
		other:             {other + "/one.ts": false, other + "/nested": true},
		other + "/nested": {other + "/nested/two.ts": false},
	}
	for dir, want := range cases {
		got := list(dir)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %v\nwant %v", dir, got, want)
		}
	}
	if got, _ := dirChildren(ks, root+"/empty"); got == nil {
		t.Error("an empty directory lists as null, want []")
	}
	for _, bad := range []string{root + "/SKILL.md", root + "/a/top.bin", root + "/nope", "skill://mcpx/missing", "skill://mcpx", root + "x"} {
		if _, ok := dirChildren(ks, bad); ok {
			t.Errorf("%s listed as a directory", bad)
		}
	}
}
