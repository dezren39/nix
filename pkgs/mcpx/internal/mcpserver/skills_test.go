package mcpserver_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// The skills extension (SEP-2640), checked against the skill files on disk
// rather than against what the server says about itself: the digests and
// sizes are recomputed here from plugin/opencode/skills, so a server that
// listed the wrong bytes, or declared the extension with nothing behind it,
// fails.

const skillsDir = "../../plugin/opencode/skills"

// diskSkills is every skill directory in the plugin, by name, with its
// SKILL.md bytes. Fails when there are none, so the checks below cannot pass
// by comparing nothing with nothing.
func diskSkills(t *testing.T) map[string][]byte {
	t.Helper()
	ents, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(skillsDir, e.Name(), "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = b
	}
	if len(out) == 0 {
		t.Fatal("no skills on disk; the premise of this test is gone")
	}
	return out
}

func TestSkillsExtensionDeclared(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	caps := resultOf(t, handle(t, s, "server/discover", modernParams(nil)))["capabilities"].(map[string]any)
	ext, _ := caps["extensions"].(map[string]any)
	if _, ok := ext[mcpserver.ExtSkills]; !ok {
		t.Fatalf("extensions = %v, want %s", ext, mcpserver.ExtSkills)
	}
	if _, ok := caps["resources"].(map[string]any); !ok {
		t.Fatal("the skills extension requires the resources capability")
	}

	// ExtrasOnly serves no mcpx_* tools, which is what the skills describe.
	s.ExtrasOnly = true
	caps = resultOf(t, handle(t, s, "server/discover", modernParams(nil)))["capabilities"].(map[string]any)
	if _, ok := caps["extensions"].(map[string]any)[mcpserver.ExtSkills]; ok {
		t.Fatal("ExtrasOnly server declares skills about tools it does not offer")
	}
}

func TestSkillsListGetRead(t *testing.T) {
	disk := diskSkills(t)
	s := mcpserver.New(newBackend(), "mcpx", "test")

	list := resultOf(t, handle(t, s, "skills/list", modernParams(nil)))
	if _, ok := list["ttlMs"]; !ok {
		t.Error("skills/list has no ttlMs on 2026-07-28")
	}
	entries, _ := list["skills"].([]any)
	if len(entries) != len(disk) {
		t.Fatalf("skills/list has %d entries, disk has %d skills", len(entries), len(disk))
	}

	listed := map[string]map[string]any{}
	for _, r := range resultOf(t, handle(t, s, "resources/list", modernParams(nil)))["resources"].([]any) {
		m := r.(map[string]any)
		listed[m["uri"].(string)] = m
	}

	for _, e := range entries {
		ent := e.(map[string]any)
		fm := ent["frontmatter"].(map[string]any)
		name := fm["name"].(string)
		body, ok := disk[name]
		if !ok {
			t.Fatalf("listed skill %q is not on disk", name)
		}
		uri := "skill://mcpx/" + name + "/SKILL.md"
		if ent["uri"] != uri {
			t.Errorf("uri = %v, want %s", ent["uri"], uri)
		}
		sum := sha256.Sum256(body)
		res := ent["resources"].([]any)[0].(map[string]any)
		if res["uri"] != uri || res["digest"] != "sha256:"+hex.EncodeToString(sum[:]) ||
			int(res["size"].(float64)) != len(body) {
			t.Errorf("%s: resources[0] = %v, does not describe the SKILL.md on disk", name, res)
		}

		got := resultOf(t, handle(t, s, "skills/get", modernParams(map[string]any{"uri": uri})))
		if got["skill"].(map[string]any)["uri"] != uri || got["nextCursor"] != nil {
			t.Errorf("skills/get %s = %v", uri, got)
		}

		read := resultOf(t, handle(t, s, "resources/read", modernParams(map[string]any{"uri": uri})))
		c := read["contents"].([]any)[0].(map[string]any)
		if c["text"] != string(body) || c["mimeType"] != "text/markdown" {
			t.Errorf("resources/read %s did not return the file on disk: %v", uri, c)
		}

		r := listed[uri]
		if r == nil || r["name"] != name || r["description"] != fm["description"] {
			t.Errorf("resources/list entry for %s = %v", uri, r)
		}
	}

	bad := handle(t, s, "skills/get", modernParams(map[string]any{"uri": "skill://mcpx/nope/SKILL.md"}))
	if errCode(bad) != -32602 {
		t.Errorf("skills/get on an unserved uri = %v, want -32602", bad)
	}
}

// diskFiles is every file of skill name on disk, keyed by its skill:// URI.
func diskFiles(t *testing.T, name string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	root := filepath.Join(skillsDir, name)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		out["skill://mcpx/"+name+"/"+filepath.ToSlash(rel)] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every file of a skill, not only its SKILL.md, is in its entry with the
// digest and size of the bytes on disk, and is readable.
func TestSkillEntriesListEveryFile(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	supporting := 0
	for name := range diskSkills(t) {
		want := diskFiles(t, name)
		supporting += len(want) - 1
		got := resultOf(t, handle(t, s, "skills/get",
			modernParams(map[string]any{"uri": "skill://mcpx/" + name + "/SKILL.md"})))["skill"].(map[string]any)["resources"].([]any)
		if len(got) != len(want) {
			t.Errorf("%s: entry lists %d files, disk has %d", name, len(got), len(want))
		}
		for _, r := range got {
			m := r.(map[string]any)
			uri := m["uri"].(string)
			body, ok := want[uri]
			sum := sha256.Sum256(body)
			if !ok || m["digest"] != "sha256:"+hex.EncodeToString(sum[:]) || int(m["size"].(float64)) != len(body) {
				t.Errorf("%s does not describe a file on disk: %v", uri, m)
			}
			c := resultOf(t, handle(t, s, "resources/read", modernParams(map[string]any{"uri": uri})))["contents"].([]any)[0].(map[string]any)
			if c["text"] != string(body) {
				t.Errorf("resources/read %s did not return the file on disk", uri)
			}
		}
	}
	if supporting == 0 {
		t.Fatal("no skill has a supporting file; this test checks nothing beyond SKILL.md")
	}
}

// resources/directory/read, checked against every directory of every skill
// on disk: its direct children, subdirectories as inode/directory, all of
// them across pages.
func TestDirectoryReadListsEveryDirectoryOnDisk(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	caps := resultOf(t, handle(t, s, "server/discover", modernParams(nil)))["capabilities"].(map[string]any)
	if ext := caps["extensions"].(map[string]any)[mcpserver.ExtSkills].(map[string]any); ext["directoryRead"] != true {
		t.Fatalf("skills extension = %v, want directoryRead: true", ext)
	}
	// One child per page, so every directory with two or more children is
	// read through nextCursor.
	s.PageSize = 1

	readAll := func(uri string) map[string]string {
		t.Helper()
		out := map[string]string{}
		params := map[string]any{"uri": uri}
		for range 1000 {
			res := resultOf(t, handle(t, s, "resources/directory/read", modernParams(params)))
			for _, r := range res["resources"].([]any) {
				m := r.(map[string]any)
				out[m["uri"].(string)], _ = m["mimeType"].(string)
			}
			next, _ := res["nextCursor"].(string)
			if next == "" {
				return out
			}
			params = map[string]any{"uri": uri, "cursor": next}
		}
		t.Fatalf("%s: pagination never ended", uri)
		return nil
	}

	dirs, subdirs := 0, 0
	for name := range diskSkills(t) {
		root := filepath.Join(skillsDir, name)
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			uri := "skill://mcpx/" + name
			if rel != "." {
				uri += "/" + filepath.ToSlash(rel)
			}
			ents, err := os.ReadDir(p)
			if err != nil {
				return err
			}
			want := map[string]bool{}
			for _, e := range ents {
				want[uri+"/"+e.Name()] = e.IsDir()
			}
			got := readAll(uri)
			dirs++
			if len(got) != len(want) {
				t.Errorf("%s: %d children listed, disk has %d: %v", uri, len(got), len(want), got)
			}
			for u, isDir := range want {
				mt, ok := got[u]
				switch {
				case !ok:
					t.Errorf("%s: child %s missing", uri, u)
				case isDir && mt != "inode/directory":
					t.Errorf("%s: subdirectory listed as %q", u, mt)
				case !isDir && (mt == "" || mt == "inode/directory"):
					t.Errorf("%s: file listed as %q", u, mt)
				}
				if isDir {
					subdirs++
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if subdirs == 0 {
		t.Fatal("no skill has a subdirectory; the inode/directory case went untested")
	}
	t.Logf("%d directories, %d subdirectories", dirs, subdirs)

	for _, bad := range []string{"skill://mcpx/mcpx-basics/SKILL.md", "skill://mcpx/nope", "demo://greeting", ""} {
		if c := errCode(handle(t, s, "resources/directory/read", modernParams(map[string]any{"uri": bad}))); c != -32602 {
			t.Errorf("directory read of %q = %d, want -32602", bad, c)
		}
	}
	// A trailing slash names the same directory.
	if got := readAll("skill://mcpx/mcpx-basics/"); len(got) == 0 {
		t.Error("a trailing slash lost the directory")
	}

	// Declared only where skills are served.
	s.ExtrasOnly = true
	if c := errCode(handle(t, s, "resources/directory/read", modernParams(map[string]any{"uri": "skill://mcpx/mcpx-basics"}))); c != -32602 {
		t.Errorf("ExtrasOnly serves a skill directory: code %d", c)
	}
}

func TestUpstreamSkillsRelayedInListAndGet(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.UpstreamSkills = func() []*mcpserver.UpstreamSkill {
		return []*mcpserver.UpstreamSkill{
			{
				Namespace:   "github",
				Name:        "pr-reviewer",
				Description: "reviews pull requests",
				Frontmatter: map[string]any{"name": "pr-reviewer", "role": "reviewer"},
				Files: []mcpserver.UpstreamSkillFile{
					{
						Path:     "SKILL.md",
						MimeType: "text/markdown; charset=utf-8",
						Body:     []byte("---\nname: pr-reviewer\n---\n# PR Reviewer"),
					},
					{
						Path:     "prompts/review.md",
						MimeType: "text/markdown; charset=utf-8",
						Body:     []byte("# Review Prompt"),
					},
				},
			},
		}
	}

	// 1. Check skills/list
	list := resultOf(t, handle(t, s, "skills/list", modernParams(nil)))
	entries, _ := list["skills"].([]any)
	foundUpstream := false
	for _, e := range entries {
		ent := e.(map[string]any)
		if ent["uri"] == "skill://github/pr-reviewer/SKILL.md" {
			foundUpstream = true
			res := ent["resources"].([]any)
			if len(res) != 2 {
				t.Fatalf("upstream skill resources length = %d, want 2", len(res))
			}
		}
	}
	if !foundUpstream {
		t.Fatal("upstream skill was not found in skills/list")
	}

	// 2. Check skills/get
	getRes := resultOf(t, handle(t, s, "skills/get", modernParams(map[string]any{"uri": "skill://github/pr-reviewer/SKILL.md"})))
	skillEntry := getRes["skill"].(map[string]any)
	if skillEntry["uri"] != "skill://github/pr-reviewer/SKILL.md" {
		t.Errorf("skills/get uri = %v, want skill://github/pr-reviewer/SKILL.md", skillEntry["uri"])
	}

	// 3. Check resources/read
	readRes := resultOf(t, handle(t, s, "resources/read", modernParams(map[string]any{"uri": "skill://github/pr-reviewer/SKILL.md"})))
	contents := readRes["contents"].([]any)[0].(map[string]any)
	if contents["text"] != "---\nname: pr-reviewer\n---\n# PR Reviewer" {
		t.Errorf("resources/read text = %v, unexpected", contents["text"])
	}

	// 4. Check resources/directory/read
	dirRes := resultOf(t, handle(t, s, "resources/directory/read", modernParams(map[string]any{"uri": "skill://github/pr-reviewer"})))
	dirResources := dirRes["resources"].([]any)
	if len(dirResources) == 0 {
		t.Fatalf("resources/directory/read for upstream skill returned 0 items")
	}
}

