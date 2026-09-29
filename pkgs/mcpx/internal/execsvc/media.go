package execsvc

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/dezren39/mcpx/internal/artifacts"
)

// interceptMedia turns inline MCP media in a result into artifact
// references.
//
// This is the local win the issue singles out, and it is worth having even
// with no remote daemon anywhere. chrome-devtools returns a screenshot as an
// `image` content block: a megabyte of base64 that goes straight into the
// agent's context, where it costs more than the rest of the task and where
// nobody will ever look at it. The bytes are the same bytes an artifact
// holds, so they are stored and the block is replaced by a resource_link the
// caller can fetch if it turns out to want it.
//
// Done here, at the output boundary, rather than inside the script. A script
// that received a handle instead of the bytes could not inspect the image it
// just took, which is a real thing scripts do -- measure it, crop it, compare
// two. So the script sees whatever the upstream server sent, and only what
// crosses back to the caller is rewritten.
func (s *Service) interceptMedia(res *Result, runID, session string) {
	if s.Store == nil {
		return
	}
	seen := map[string]bool{}
	for _, a := range res.Artifacts {
		seen[a.ID] = true
	}
	capture := func(m artifacts.Meta) {
		if seen[m.ID] {
			return
		}
		seen[m.ID] = true
		res.Artifacts = append(res.Artifacts, Artifact{Meta: m})
	}

	if res.Result != nil {
		res.Result = s.rewrite(res.Result, runID, session, capture)
	}
	for i, raw := range res.Emits {
		var v any
		if json.Unmarshal(raw, &v) != nil {
			continue
		}
		out := s.rewrite(v, runID, session, capture)
		if b, err := json.Marshal(out); err == nil {
			res.Emits[i] = b
		}
	}
}

// rewrite walks a decoded JSON value replacing media blocks.
//
// Depth is not bounded because the value came from json.Unmarshal, which
// already refuses to build a structure deeper than it can decode.
func (s *Service) rewrite(v any, runID, session string, capture func(artifacts.Meta)) any {
	switch x := v.(type) {
	case []any:
		for i, item := range x {
			x[i] = s.rewrite(item, runID, session, capture)
		}
		return x
	case map[string]any:
		if name, mime, data, ok := mediaBlock(x); ok {
			if meta, err := s.storeMedia(name, mime, data, runID, session); err == nil {
				capture(meta)
				return map[string]any{
					"type":     "resource_link",
					"uri":      meta.URI,
					"name":     meta.Name,
					"mimeType": meta.Mime,
					// Size is carried because the only question a caller has
					// before deciding to fetch is how much it costs.
					"size": meta.Size,
				}
			}
		}
		for k, item := range x {
			x[k] = s.rewrite(item, runID, session, capture)
		}
		return x
	}
	return v
}

// mediaBlock recognises the MCP content shapes that carry binary bodies.
//
// Read from the schema rather than from prose: ImageContent and AudioContent
// both have a required base64 `data` and a required `mimeType`, and
// EmbeddedResource carries a BlobResourceContents with `blob` and a `uri`.
// Text content is left alone -- it is already the cheap form.
func mediaBlock(x map[string]any) (name, mime, data string, ok bool) {
	t, _ := x["type"].(string)
	switch t {
	case "image", "audio":
		d, _ := x["data"].(string)
		if d == "" {
			return "", "", "", false
		}
		mime, _ = x["mimeType"].(string)
		name, _ = x["name"].(string)
		if name == "" {
			name = t + extensionFor(mime)
		}
		return name, mime, d, true
	case "resource":
		r, _ := x["resource"].(map[string]any)
		if r == nil {
			return "", "", "", false
		}
		blob, _ := r["blob"].(string)
		if blob == "" {
			return "", "", "", false
		}
		mime, _ = r["mimeType"].(string)
		uri, _ := r["uri"].(string)
		name = uri[strings.LastIndexByte(uri, '/')+1:]
		if name == "" {
			name = "resource" + extensionFor(mime)
		}
		return name, mime, blob, true
	}
	return "", "", "", false
}

func (s *Service) storeMedia(name, mime, b64, runID, session string) (artifacts.Meta, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return artifacts.Meta{}, err
	}
	return s.Store.Put(strings.NewReader(string(raw)), artifacts.PutOptions{
		Name: name, Mime: mime, Run: runID, Session: session,
	})
}

// extensionFor names a file after its type, so the artifact that lands in a
// caller's directory opens in whatever opens that type.
func extensionFor(mime string) string {
	mime, _, _ = strings.Cut(mime, ";")
	switch strings.TrimSpace(strings.ToLower(mime)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "application/pdf":
		return ".pdf"
	}
	return ".bin"
}
