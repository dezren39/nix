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
