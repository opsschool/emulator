package hosted

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opsschool/emulator/internal/results"
	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/session"
)

type fakeBackend struct {
	mu       sync.Mutex
	sessions map[string]Session
	deleted  []string
}

func (f *fakeBackend) Create(ctx context.Context, s Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions == nil {
		f.sessions = map[string]Session{}
	}
	f.sessions[s.ID] = s
	return nil
}

func (f *fakeBackend) List(ctx context.Context) ([]Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Session
	for _, s := range f.sessions {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeBackend) Delete(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeBackend) update(id string, fn func(*Session)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[id]
	fn(&s)
	f.sessions[id] = s
}

func testScenarios(t *testing.T) []*scenario.Scenario {
	t.Helper()
	scs, errs := scenario.LoadAll("../../scenarios")
	if len(errs) > 0 || len(scs) == 0 {
		t.Fatalf("loading scenarios: %v", errs)
	}
	return scs
}

func newTestPortal(t *testing.T) (*Portal, *fakeBackend, *httptest.Server) {
	t.Helper()
	be := &fakeBackend{}
	p := &Portal{
		Scenarios: testScenarios(t), Store: results.Open(t.TempDir()), Backend: be,
		MaxSessions: 2, Log: log.New(io.Discard, "", 0),
	}
	p.refresh(context.Background())
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	return p, be, srv
}

// learner is a browser with its own cookies.
type learner struct {
	t   *testing.T
	srv *httptest.Server
	c   *http.Client
}

func newLearner(t *testing.T, srv *httptest.Server, name string) *learner {
	jar, _ := cookiejar.New(nil)
	l := &learner{t, srv, &http.Client{Jar: jar}}
	if name != "" {
		if code, body := l.do("POST", "/api/me", `{"user":"`+name+`"}`); code != 200 {
			t.Fatalf("naming %s: %d %s", name, code, body)
		}
	}
	return l
}

func (l *learner) do(method, path, body string) (int, string) {
	l.t.Helper()
	req, _ := http.NewRequest(method, l.srv.URL+path, strings.NewReader(body))
	if method != "GET" {
		req.Header.Set("Origin", l.srv.URL)
	}
	resp, err := l.c.Do(req)
	if err != nil {
		l.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPortalPages(t *testing.T) {
	_, _, srv := newTestPortal(t)
	anon := newLearner(t, srv, "")
	for _, path := range []string{"/", "/portal.js", "/portal.css", "/ui/app.css", "/ui/fonts/AtkinsonHyperlegibleNext.woff2", "/healthz"} {
		if code, _ := anon.do("GET", path, ""); code != 200 {
			t.Errorf("%s: %d", path, code)
		}
	}
	if code, _ := anon.do("GET", "/api/scenarios", ""); code != http.StatusUnauthorized {
		t.Errorf("scenarios without a name: %d", code)
	}
	if code, _ := anon.do("POST", "/api/me", `{"user":"<script>"}`); code != http.StatusBadRequest {
		t.Errorf("a bad name: %d", code)
	}
	sam := newLearner(t, srv, "sam")
	code, body := sam.do("GET", "/api/scenarios", "")
	var rows []ScenarioRow
	if code != 200 || json.Unmarshal([]byte(body), &rows) != nil || len(rows) == 0 || rows[0].Level == 0 {
		t.Fatalf("scenarios: %d %s", code, body)
	}
}

func TestPortalSessions(t *testing.T) {
	p, be, srv := newTestPortal(t)
	sam := newLearner(t, srv, "sam")
	code, body := sam.do("POST", "/api/sessions", `{"scenario":"linux/1.1"}`)
	if code != 200 {
		t.Fatalf("create: %d %s", code, body)
	}
	var s Session
	json.Unmarshal([]byte(body), &s)
	if s.ID == "" || be.sessions[s.ID].Token == "" || be.sessions[s.ID].User != "sam" {
		t.Fatalf("created %+v, backend has %+v", s, be.sessions)
	}
	if !strings.HasSuffix(be.sessions[s.ID].GrafanaRoot, "/s/"+s.ID+"/grafana/") {
		t.Errorf("grafana root %q", be.sessions[s.ID].GrafanaRoot)
	}
	if code, _ := sam.do("POST", "/api/sessions", `{"scenario":"linux/2.1"}`); code != http.StatusConflict {
		t.Errorf("a second session: %d", code)
	}
	if code, _ := sam.do("POST", "/api/sessions", `{"scenario":"nope/1.1"}`); code != http.StatusNotFound {
		t.Errorf("an unknown scenario: %d", code)
	}
	newLearner(t, srv, "alex").do("POST", "/api/sessions", `{"scenario":"linux/1.1"}`)
	if code, _ := newLearner(t, srv, "kim").do("POST", "/api/sessions", `{"scenario":"linux/1.1"}`); code != http.StatusServiceUnavailable {
		t.Errorf("over the session cap: %d", code)
	}
	if _, body := sam.do("GET", "/api/session", ""); !strings.Contains(body, s.ID) {
		t.Errorf("my session: %s", body)
	}

	// The page loads at once; status says it's starting.
	if code, body := sam.do("GET", "/s/"+s.ID+"/", ""); code != 200 || !strings.Contains(body, "app.js") {
		t.Errorf("session page: %d", code)
	}
	code, body = sam.do("GET", "/s/"+s.ID+"/status", "")
	var st Starting
	if code != http.StatusServiceUnavailable || json.Unmarshal([]byte(body), &st) != nil || !st.Starting {
		t.Errorf("status while pending: %d %s", code, body)
	}
	// Nobody else's.
	alex := newLearner(t, srv, "alex")
	if code, body := alex.do("GET", "/s/"+s.ID+"/status", ""); code != http.StatusServiceUnavailable || !strings.Contains(body, "isn't yours") {
		t.Errorf("someone else's status: %d %s", code, body)
	}
	if code, _ := alex.do("POST", "/s/"+s.ID+"/stop", ""); code != http.StatusNotFound {
		t.Errorf("someone else's stop: %d", code)
	}

	// Once running, calls go to the runner with the token, and the Host
	// the browser used.
	var got *http.Request
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		io.WriteString(w, `{"ok":true}`)
	}))
	defer runner.Close()
	p.transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("tcp", runner.Listener.Addr().String())
	}}
	be.update(s.ID, func(s *Session) { s.Phase, s.Addr = "Running", "10.0.0.9" })
	p.refresh(context.Background())
	code, body = sam.do("POST", "/s/"+s.ID+"/hint", "")
	if code != 200 || got == nil {
		t.Fatalf("proxied hint: %d %s", code, body)
	}
	if got.URL.Path != "/hint" || got.Header.Get(session.TokenHeader) != be.sessions[s.ID].Token || got.Host != strings.TrimPrefix(srv.URL, "http://") {
		t.Errorf("runner got %s %s token %q host %q", got.Method, got.URL.Path, got.Header.Get(session.TokenHeader), got.Host)
	}
	sam.do("GET", "/s/"+s.ID+"/grafana/d/scenario", "")
	if got.URL.Path != "/s/"+s.ID+"/grafana/d/scenario" {
		t.Errorf("grafana path %q", got.URL.Path)
	}

	// Another site can't drive the session.
	req, _ := http.NewRequest("POST", srv.URL+"/s/"+s.ID+"/stop", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := sam.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site stop: %d", resp.StatusCode)
	}

	// Discarding removes it.
	if code, _ := sam.do("DELETE", "/api/sessions/"+s.ID, ""); code != 200 {
		t.Errorf("discard: %d", code)
	}
	if _, body := sam.do("GET", "/api/session", ""); strings.TrimSpace(body) != "null" {
		t.Errorf("after discarding: %s", body)
	}
}

func TestPortalResults(t *testing.T) {
	p, be, srv := newTestPortal(t)
	sam := newLearner(t, srv, "sam")
	_, body := sam.do("POST", "/api/sessions", `{"scenario":"linux/1.1"}`)
	var s Session
	json.Unmarshal([]byte(body), &s)
	token := be.sessions[s.ID].Token

	post := func(token, body string) int {
		req, _ := http.NewRequest("POST", srv.URL+"/internal/results", strings.NewReader(body))
		req.Header.Set(session.TokenHeader, token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	res := `{"user":"mallory","scenario":"linux/2.1","score":200,"tier_passed":{"mitigated":60,"fixed":300}}`
	if code := post("wrong", res); code != http.StatusForbidden {
		t.Errorf("wrong token: %d", code)
	}
	if code := post(token, res); code != 200 {
		t.Fatalf("result: %d", code)
	}
	post(token, res) // sent twice: recorded once
	rs, _ := p.Store.All()
	if len(rs) != 1 || rs[0].User != "sam" || rs[0].Scenario != "linux/1.1" || rs[0].Score != 200 {
		t.Fatalf("stored %+v", rs)
	}
	_, body = sam.do("GET", "/api/scoreboard", "")
	var sb Scoreboard
	json.Unmarshal([]byte(body), &sb)
	if len(sb.Learners) != 1 || sb.Learners[0].Points != 200 || sb.Learners[0].Fixed != 1 {
		t.Errorf("scoreboard learners %+v", sb.Learners)
	}
	for _, f := range sb.Scenarios {
		if f.ID == "linux/1.1" && (f.User != "sam" || f.Fixed != 300 || f.Mitigated != 60) {
			t.Errorf("fastest linux/1.1: %+v", f)
		}
	}
}

func TestPortalReapsSessions(t *testing.T) {
	p, be, _ := newTestPortal(t)
	p.MaxAge = time.Hour
	be.Create(context.Background(), Session{ID: "done", User: "a", Phase: "Succeeded", Created: time.Now()})
	be.Create(context.Background(), Session{ID: "old", User: "b", Phase: "Running", Created: time.Now().Add(-2 * time.Hour)})
	be.Create(context.Background(), Session{ID: "live", User: "c", Phase: "Running", Created: time.Now()})
	p.refresh(context.Background())
	if len(be.deleted) != 1 || be.deleted[0] != "old" {
		t.Fatalf("first pass deleted %v; an ended session stays a minute for its page", be.deleted)
	}
	p.endedAt["done"] = time.Now().Add(-2 * time.Minute)
	p.refresh(context.Background())
	if len(be.deleted) != 2 || be.deleted[1] != "done" {
		t.Errorf("deleted %v", be.deleted)
	}
}

func TestBuildScoreboard(t *testing.T) {
	scs := testScenarios(t)
	rs := []results.Result{
		{User: "a", Scenario: "linux/1.1", Score: 100, TierPassed: map[string]results.Seconds{"mitigated": 50}},
		{User: "a", Scenario: "linux/1.1", Score: 250, TierPassed: map[string]results.Seconds{"mitigated": 90, "fixed": 200}},
		{User: "b", Scenario: "linux/1.1", Score: 200, TierPassed: map[string]results.Seconds{"mitigated": 40, "fixed": 100}},
		{User: "b", Scenario: "linux/2.1", Score: 100, TierPassed: map[string]results.Seconds{"mitigated": 40}},
	}
	sb := BuildScoreboard(rs, scs)
	if len(sb.Learners) != 2 || sb.Learners[0].User != "b" || sb.Learners[0].Points != 300 || sb.Learners[1].Points != 250 {
		t.Errorf("learners %+v", sb.Learners)
	}
	if len(sb.Scenarios) != len(scs) {
		t.Errorf("%d scenario rows for %d scenarios", len(sb.Scenarios), len(scs))
	}
	for _, f := range sb.Scenarios {
		if f.ID == "linux/1.1" && (f.User != "b" || f.Fixed != 100) {
			t.Errorf("fastest linux/1.1 %+v", f)
		}
	}
}
