package mcpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/dezren39/mcpx/plugin/opencode/skills"
)

// The skills extension (SEP-2640): mcpx serves its own skills.
//
// A skill is a directory with a SKILL.md at its root, the format opencode and
// other agents already load from disk. The extension lets a server publish
// them: skills/list and skills/get describe each skill -- its SKILL.md URI, its
// frontmatter, and a digest and size for every file -- and the files
// themselves are ordinary resources, read with resources/read.
//
// What mcpx serves is the skills it ships in plugin/opencode/skills, which
// explain how to use mcpx itself. They used to reach only an opencode user who
// copied them into place by hand; served here, any MCP host connected to mcpx
// gets them, and they cannot drift from the binary they describe.
//
// Upstream servers' skills are not relayed through skills/list; see
// docs/in-name-only.md. Their skill files still pass through as resources.

// ExtSkills is the skills extension's identifier.
const ExtSkills = "io.modelcontextprotocol/skills"

// skillAuthority is the first <skill-path> segment of every skill mcpx
// serves, so its URIs cannot be mistaken for a pass-through upstream's
// skill:// resources.
const skillAuthority = "mcpx"

var skillNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type skillFile struct {
	uri  string
	mime string
	body []byte
}

type skill struct {
	name, description string
	uri               string // the SKILL.md
	frontmatter       map[string]any
	files             []skillFile // SKILL.md first
}

// entry is the skill in the shape skills/list and skills/get share.
func (k *skill) entry() map[string]any {
	res := make([]any, 0, len(k.files))
	for _, f := range k.files {
		sum := sha256.Sum256(f.body)
		res = append(res, map[string]any{"uri": f.uri,
			"digest": "sha256:" + hex.EncodeToString(sum[:]), "size": len(f.body)})
	}
	return map[string]any{"uri": k.uri, "frontmatter": k.frontmatter, "resources": res}
}

var ownSkills = sync.OnceValues(func() ([]*skill, error) { return loadSkills(skills.FS) })

// loadSkills reads every top-level directory of fsys as a skill.
func loadSkills(fsys fs.FS) ([]*skill, error) {
	dirs, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []*skill
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		k, err := loadSkill(fsys, d.Name())
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", d.Name(), err)
		}
		out = append(out, k)
	}
	return out, nil
}

func loadSkill(fsys fs.FS, name string) (*skill, error) {
	if !skillNameRE.MatchString(name) || len(name) > 64 {
		return nil, fmt.Errorf("%q is not a valid skill name", name)
	}
	root := "skill://" + skillAuthority + "/" + name
	md, err := fs.ReadFile(fsys, name+"/SKILL.md")
	if err != nil {
		return nil, err
	}
	fm, err := frontmatter(md)
	if err != nil {
		return nil, err
	}
	// The extension requires the directory name to be the skill's name: it
	// is how a host recovers the name from the URI alone.
	if fm["name"] != name {
		return nil, fmt.Errorf("frontmatter name %v does not match its directory", fm["name"])
	}
	desc, _ := fm["description"].(string)
	if desc == "" {
		return nil, fmt.Errorf("frontmatter has no description")
	}
	k := &skill{name: name, description: desc, uri: root + "/SKILL.md", frontmatter: fm,
		files: []skillFile{{uri: root + "/SKILL.md", mime: "text/markdown", body: md}}}
	err = fs.WalkDir(fsys, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == name+"/SKILL.md" {
			return err
		}
		body, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		mt := mime.TypeByExtension(path.Ext(p))
		if mt == "" {
			mt = "application/octet-stream"
			if utf8.Valid(body) {
				mt = "text/plain"
			}
		}
		k.files = append(k.files, skillFile{uri: root + "/" + strings.TrimPrefix(p, name+"/"), mime: mt, body: body})
		return nil
	})
	return k, err
}

// frontmatter parses the YAML block a SKILL.md begins with.
func frontmatter(md []byte) (map[string]any, error) {
	md = bytes.TrimPrefix(md, []byte("\ufeff"))
	md = bytes.ReplaceAll(md, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(md, []byte("---\n"))
	if !ok {
		return nil, fmt.Errorf("SKILL.md does not begin with --- frontmatter")
	}
	block, _, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return nil, fmt.Errorf("SKILL.md frontmatter is not closed")
	}
	var fm map[string]any
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return nil, fmt.Errorf("SKILL.md frontmatter: %w", err)
	}
	if fm == nil {
		return nil, fmt.Errorf("SKILL.md frontmatter is empty")
	}
	return fm, nil
}

// skills is what this server serves: mcpx's own skills, unless the server
// offers only contributed tools, in which case the skills -- which describe
// the mcpx_* tools -- would describe tools that are not there.
func (s *Server) skills() []*skill {
	if s.ExtrasOnly {
		return nil
	}
	ks, _ := ownSkills()
	return ks
}

// skillResources is every skill file as a resources/list entry. The SKILL.md
// carries the skill's name and description, as the extension asks.
func (s *Server) skillResources() []ResourceRef {
	var out []ResourceRef
	for _, k := range s.skills() {
		for i, f := range k.files {
			size := int64(len(f.body))
			r := ResourceRef{URI: f.uri, Name: path.Base(f.uri), MimeType: f.mime, Size: &size}
			if i == 0 {
				r.Name, r.Description = k.name, k.description
			}
			out = append(out, r)
		}
	}
	return out
}

// readSkillFile answers resources/read for a skill file, or reports false.
func (s *Server) readSkillFile(uri string) ([]ResourceContents, bool) {
	for _, k := range s.skills() {
		for _, f := range k.files {
			if f.uri != uri {
				continue
			}
			c := ResourceContents{URI: uri, MimeType: f.mime}
			if utf8.Valid(f.body) {
				c.Text = string(f.body)
			} else {
				c.Blob = base64.StdEncoding.EncodeToString(f.body)
			}
			return []ResourceContents{c}, true
		}
	}
	return nil, false
}

// handleSkills answers skills/list and skills/get.
func (s *Server) handleSkills(_ context.Context, req request) *response {
	fail := func(code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: code, Message: msg}}
	}
	reply := func(v any) *response { return &response{JSONRPC: "2.0", ID: req.ID, Result: v} }
	ks := s.skills()
	if req.Method == "skills/get" {
		var p struct {
			URI string `json:"uri"`
		}
		if len(req.Params) == 0 || json.Unmarshal(req.Params, &p) != nil || p.URI == "" {
			return fail(codeInvalidParams, "skills/get needs a uri")
		}
		for _, k := range ks {
			if k.uri == p.URI {
				return reply(map[string]any{"skill": k.entry()})
			}
		}
		return fail(codeInvalidParams, fmt.Sprintf("no skill %q", p.URI))
	}
	sorted := append([]*skill(nil), ks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].uri < sorted[j].uri })
	items, next, err := page(sorted, req.Params, s.pageSize())
	if err != nil {
		return fail(codeInvalidParams, err.Error())
	}
	entries := make([]any, 0, len(items))
	for _, k := range items {
		entries = append(entries, k.entry())
	}
	out := map[string]any{"skills": entries}
	if next != "" {
		out["nextCursor"] = next
	}
	return reply(out)
}
