package e2e_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// PUT /v1/settings is the fourth way upstream.protocol is read; an old name
// sent there is stored and reported as the canonical one, and an unknown one
// is refused with the accepted spellings.
func TestPutSettingsNormalisesAProtocolAlias(t *testing.T) {
	e, _ := eraEnv(t, "modern", "")
	e.run("call", "era.hello", "{}")
	c := e.socketClient(t)
	put := func(v string) (int, string) {
		req, _ := http.NewRequest(http.MethodPut, "http://mcpx/v1/settings/upstream.protocol",
			strings.NewReader(`{"value":"`+v+`"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(b)
	}
	code, body := put("force-legacy")
	if code != http.StatusOK || !strings.Contains(body, `"value":"force-initialize"`) {
		t.Errorf("PUT force-legacy: want 200 with value force-initialize, got %d: %s", code, body)
	}
	resp, err := c.Get("http://mcpx/v1/settings/upstream.protocol")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// upstream.protocol is not hot, so the PUT does not change the running
	// value; GET still has to say which spellings it would accept.
	if !strings.Contains(string(b), `"prefer-initialize":["legacy",`) {
		t.Errorf("GET should list the aliases: %s", b)
	}
	code, body = put("newest")
	if code != http.StatusBadRequest || !strings.Contains(body, "prefer-initialize") ||
		!strings.Contains(body, "force-session") {
		t.Errorf("PUT newest: want 400 listing names and aliases, got %d: %s", code, body)
	}
}
