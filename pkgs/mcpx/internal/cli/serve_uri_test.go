package cli

import (
	"net/url"
	"regexp"
	"testing"
)

// The URI mcpx lists an upstream resource under must itself be a URI, and
// must name exactly the upstream resource it came from.
// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#custom-uri-schemes
func TestMCPXResourceURIs(t *testing.T) {
	// RFC 3986 path characters: pchar, "/" and percent-encodings only.
	pathOK := regexp.MustCompile(`^(?:[A-Za-z0-9\-._~!$&'()*+,;=:@/]|%[0-9A-F]{2})*$`)
	cases := []string{
		"file:///home/me/notes.md",
		"/abs/doc",
		"file:///a b/c#frag?q=1",
		"file:///already%20encoded",
		"db://host/table?id=5&x=ü",
		"weird:%zz",
	}
	for _, rev := range []string{"2025-06-18", "2025-11-25", "2026-07-28"} {
		t.Run(rev+"/resources/resources-custom-scheme-rfc3986", func(t *testing.T) {
			for _, up := range cases {
				u := mcpxURI("ns", up)
				p, err := url.Parse(u)
				if err != nil || p.Scheme != "mcpx" || p.Host != "ns" || p.RawQuery != "" || p.Fragment != "" {
					t.Errorf("%q -> %q is not one mcpx URI: %v", up, u, err)
					continue
				}
				if !pathOK.MatchString(u[len("mcpx://ns"):]) {
					t.Errorf("%q -> %q has characters a path may not", up, u)
				}
				ns, back, ok := splitMCPXURI(u)
				want := up
				if want[0] == '/' {
					want = want[1:]
				}
				if !ok || ns != "ns" || back != want {
					t.Errorf("%q -> %q -> %q %q %v", up, u, ns, back, ok)
				}
			}
			if u := mcpxURI("ns", "file:///home/me/notes.md"); u != "mcpx://ns/file:///home/me/notes.md" {
				t.Errorf("an ordinary URI changed: %q", u)
			}
		})
	}
}
