package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestHandleDaemonRestart(t *testing.T) {
	execSelf = func(string, []string, []string) error { return errors.New("exec is disabled in tests") }
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	defer signal.Stop(term)

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/v1/daemon/restart", nil)
	w := httptest.NewRecorder()

	s.handleDaemonRestart(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res["status"] != "restarting" {
		t.Fatalf("expected status=restarting, got: %v", res["status"])
	}

	select {
	case <-term:
	case <-time.After(5 * time.Second):
		t.Fatal("the restart handler never finished")
	}
}
