package hosted

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRunnerPod(t *testing.T) {
	k := &Kube{Namespace: "ns", Image: "reg/opsschool:1", MachineImage: "reg/{image}:base", PortalURL: "http://portal", ServiceAccount: "sa", RuntimeClass: "kata"}
	b, err := json.Marshal(k.RunnerPod(Session{ID: "abc", User: "sam", Scenario: "linux/1.1", Token: "tok", GrafanaRoot: "https://x/s/abc/grafana/"}))
	if err != nil {
		t.Fatal(err)
	}
	pod := string(b)
	for _, want := range []string{
		`"name":"opsschool-session-abc"`, `"opsschool.org/token":"tok"`, `"serviceAccountName":"sa"`,
		`"name":"OPSSCHOOL_MACHINE_POD","value":"opsschool-machine-abc"`, `"name":"OPSSCHOOL_RUNTIME_CLASS","value":"kata"`,
		`"name":"GF_SERVER_ROOT_URL","value":"https://x/s/abc/grafana/"`, `"command":["opsschool","_runner"]`,
		`"command":["opsschool","_render-telemetry","linux/1.1","/telemetry"]`,
	} {
		if !strings.Contains(pod, want) {
			t.Errorf("runner pod lacks %s", want)
		}
	}
	// The pod must finish when the runner does: telemetry are sidecars.
	if n := strings.Count(pod, `"restartPolicy":"Always"`); n != 3 {
		t.Errorf("%d sidecars, want 3", n)
	}
}

func TestParsePods(t *testing.T) {
	ss, err := parsePods([]byte(`{"items":[
	 {"metadata":{"labels":{"opsschool.org/session":"a"},"annotations":{"opsschool.org/user":"sam","opsschool.org/token":"t"},"creationTimestamp":"2026-10-05T00:00:00Z"},
	  "status":{"phase":"Pending","containerStatuses":[{"name":"runner","state":{"waiting":{"reason":"ErrImagePull"}}}]}},
	 {"metadata":{"labels":{"opsschool.org/session":"b"}},
	  "status":{"phase":"Running","podIP":"10.0.0.2","containerStatuses":[{"name":"runner","state":{"terminated":{"exitCode":0}}}]}},
	 {"metadata":{"labels":{"opsschool.org/session":"c"}},
	  "status":{"phase":"Failed","containerStatuses":[{"name":"runner","state":{"terminated":{"exitCode":1,"message":"break.sh failed\n"}}}]}},
	 {"metadata":{"labels":{}},"status":{"phase":"Running"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 3 || ss[0].User != "sam" || ss[0].Waiting != "ErrImagePull" || ss[0].Token != "t" {
		t.Fatalf("parsed %+v", ss)
	}
	if !ss[1].Ended() || ss[1].Addr != "10.0.0.2" {
		t.Errorf("a pod whose runner exited should count as ended: %+v", ss[1])
	}
	if ss[2].Phase != "Failed" || ss[2].Error != "break.sh failed" {
		t.Errorf("a failed runner's message should be kept: %+v", ss[2])
	}
	if !strings.Contains(pendingMessage(ss[0]), "trouble downloading") {
		t.Errorf("pending message %q", pendingMessage(ss[0]))
	}
}
