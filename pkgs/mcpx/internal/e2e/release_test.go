package e2e_test

import (
	"encoding/json"
	"testing"
)

func TestASessionlessExecLeavesASharedInstanceRunning(t *testing.T) {
	// The release at the end of an exec is for what the run minted for
	// itself -- its session's instance. It stopped the shared one too, so
	// every exec after the first paid a cold start.
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"warm it"}`)
	pidOf := func() int {
		var st struct {
			Servers []struct {
				Instances []struct {
					PID int `json:"pid"`
				} `json:"instances"`
			} `json:"servers"`
		}
		if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
			t.Fatal(err)
		}
		if len(st.Servers) != 1 || len(st.Servers[0].Instances) != 1 {
			t.Fatalf("expected exactly one live shared instance: %+v", st)
		}
		return st.Servers[0].Instances[0].PID
	}
	before := pidOf()
	e.run("exec", `console.log(String(await demo.echo({ message: "x" })));`)
	if after := pidOf(); after != before {
		t.Fatalf("the shared instance was replaced: pid %d, then %d", before, after)
	}
}
