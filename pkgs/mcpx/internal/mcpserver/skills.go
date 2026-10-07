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
	root              string      // skill://mcpx/<name>, the skill's directory
	dirs              []string    // every directory's URI, root included
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
		files: []skillFile{{uri: root + "/SKILL.md", mime: "text/markdown", body: md}}, root: root}
	err = fs.WalkDir(fsys, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == name+"/SKILL.md" {
			return err
		}
		if d.IsDir() {
			// Recorded separately from the files so an empty directory is
			// still a directory resources/directory/read can list.
			k.dirs = append(k.dirs, root+strings.TrimPrefix(p, name))
			return nil
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

// UpstreamSkill represents a skill contributed by an upstream server.
type UpstreamSkill struct {
	Namespace   string         `json:"namespace"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Files       []UpstreamSkillFile `json:"files,omitempty"`
}

// UpstreamSkillFile is a single file belonging to an upstream skill.
type UpstreamSkillFile struct {
	Path     string `json:"path"`
	MimeType string `json:"mimeType,omitempty"`
	Body     []byte `json:"body"`
}

func convertUpstreamSkill(us *UpstreamSkill) *skill {
	if us == nil || us.Name == "" {
		return nil
	}
	authority := us.Namespace
	if authority == "" {
		authority = "upstream"
	}
	root := "skill://" + authority + "/" + us.Name
	k := &skill{
		name:        us.Name,
		description: us.Description,
		uri:         root + "/SKILL.md",
		frontmatter: us.Frontmatter,
		root:        root,
	}
	k.dirs = append(k.dirs, root)
	dirSeen := map[string]bool{root: true}

	for _, f := range us.Files {
		rel := strings.TrimPrefix(f.Path, "/")
		fileURI := root + "/" + rel
		m := f.MimeType
		if m == "" {
			m = mime.TypeByExtension(path.Ext(rel))
			if m == "" {
				m = "text/markdown; charset=utf-8"
			}
		}
		k.files = append(k.files, skillFile{
			uri:  fileURI,
			mime: m,
			body: f.Body,
		})
		parent := path.Dir(rel)
		for parent != "." && parent != "/" && parent != "" {
			dirURI := root + "/" + parent
			if !dirSeen[dirURI] {
				dirSeen[dirURI] = true
				k.dirs = append(k.dirs, dirURI)
			}
			parent = path.Dir(parent)
		}
	}
	sort.Strings(k.dirs)
	return k
}

// skills is what this server serves: mcpx's own skills, plus any upstream skills.
func (s *Server) skills() []*skill {
	if s.ExtrasOnly {
		return nil
	}
	ks, _ := ownSkills()
	out := append([]*skill(nil), ks...)
	if s.UpstreamSkills != nil {
		for _, us := range s.UpstreamSkills() {
			if k := convertUpstreamSkill(us); k != nil {
				out = append(out, k)
			}
		}
	}
	return out
}

// fileRef is a skill file as a resource listing entry. The SKILL.md
// carries the skill's name and description, as the extension asks.
func (k *skill) fileRef(i int) ResourceRef {
	f := k.files[i]
	size := int64(len(f.body))
	r := ResourceRef{URI: f.uri, Name: path.Base(f.uri), MimeType: f.mime, Size: &size}
	if i == 0 {
		r.Name, r.Description = k.name, k.description
	}
	return r
}

// skillResources is every skill file as a resources/list entry. The SKILL.md
// carries the skill's name and description, as the extension asks.
func (s *Server) skillResources() []ResourceRef {
	var out []ResourceRef
	for _, k := range s.skills() {
		for i := range k.files {
			out = append(out, k.fileRef(i))
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

// dirMime marks a directory resource (SEP-2640, Directory Listing).
const dirMime = "inode/directory"

// handleDirectoryRead answers resources/directory/read: the direct children
// of a directory inside a served skill, files with the metadata
// resources/list gives them and subdirectories as inode/directory
// resources. Not recursive; a client descends by asking again. Anything that
// is not a served directory -- a file, an unknown URI -- is -32602, the code
// resources/read uses for an unknown resource.
func (s *Server) handleDirectoryRead(_ context.Context, req request) *response {
	fail := func(msg string) *response {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidParams, Message: msg}}
	}
	var p struct {
		URI string `json:"uri"`
	}
	if len(req.Params) == 0 || json.Unmarshal(req.Params, &p) != nil || p.URI == "" {
		return fail("resources/directory/read needs a uri")
	}
	// A trailing slash names the same directory.
	dir := strings.TrimSuffix(p.URI, "/")
	children, ok := dirChildren(s.skills(), dir)
	if !ok {
		return fail(fmt.Sprintf("%q is not a directory this server serves", p.URI))
	}
	items, next, err := page(children, req.Params, s.pageSize())
	if err != nil {
		return fail(err.Error())
	}
	out := map[string]any{"resources": items}
	if next != "" {
		out["nextCursor"] = next
	}
	return &response{JSONRPC: "2.0", ID: req.ID, Result: out}
}

// dirChildren lists dir's direct children sorted by URI, or reports false
// when dir is not a directory of one of ks.
func dirChildren(ks []*skill, dir string) ([]ResourceRef, bool) {
	for _, k := range ks {
		if dir != k.root && !strings.HasPrefix(dir, k.root+"/") {
			continue
		}
		known := false
		var out []ResourceRef
		for _, d := range k.dirs {
			if d == dir {
				known = true
			} else if parentURI(d) == dir {
				out = append(out, ResourceRef{URI: d, Name: path.Base(d), MimeType: dirMime})
			}
		}
		if !known {
			return nil, false
		}
		for i, f := range k.files {
			if parentURI(f.uri) == dir {
				out = append(out, k.fileRef(i))
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
		if out == nil {
			out = []ResourceRef{} // an empty directory is an empty array, not null
		}
		return out, true
	}
	return nil, false
}

// parentURI is the URI up to its last slash. Not path.Dir, which cleans the
// scheme's "//" down to one.
func parentURI(u string) string { return u[:max(strings.LastIndexByte(u, '/'), 0)] }
