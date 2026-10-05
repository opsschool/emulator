package hosted

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/opsschool/emulator/internal/results"
	"github.com/opsschool/emulator/internal/scenario"
	"github.com/opsschool/emulator/internal/session"
	"github.com/opsschool/emulator/internal/telemetry"
	"github.com/opsschool/emulator/internal/webui"
)

//go:embed static
var static embed.FS

// Portal is the hosted emulator's web front end. Learners pick a scenario,
// play it on the session page, and compare results on a shared scoreboard.
type Portal struct {
	Scenarios []*scenario.Scenario
	Store     *results.Store
	Backend   Backend
	// UserHeader names the request header that an authenticating proxy in
	// front of the portal sets to the learner's name, such as
	// X-Forwarded-Email. Empty means learners type their own name.
	UserHeader string
	// PublicURL is the portal's address as browsers see it, such as
	// https://opsschool.example.com. Optional; the request's is used.
	PublicURL string
	// MaxSessions caps the sessions running at once. 0 means no cap.
	MaxSessions int
	// MaxAge deletes sessions older than this, a backstop for runners
	// that never end. Runners end themselves 10 minutes after the time
	// limit runs out.
	MaxAge time.Duration
	Log    *log.Logger

	// transport reaches session pods; tests replace it.
	transport http.RoundTripper

	mu       sync.Mutex
	sessions map[string]Session
	listed   bool
	endedAt  map[string]time.Time // when a session was first seen ended
	recorded map[string]bool      // sessions whose result is saved
	creating map[string]bool      // users with a session being created
}

// Run keeps the session list fresh and removes ended sessions until ctx
// ends.
func (p *Portal) Run(ctx context.Context) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		p.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (p *Portal) refresh(ctx context.Context) {
	ss, err := p.Backend.List(ctx)
	if err != nil {
		p.Log.Printf("listing sessions: %v", err)
		return
	}
	now := time.Now()
	p.mu.Lock()
	if p.endedAt == nil {
		p.endedAt = map[string]time.Time{}
	}
	p.sessions = map[string]Session{}
	var reap []string
	for _, s := range ss {
		p.sessions[s.ID] = s
		switch {
		case s.Ended():
			if _, ok := p.endedAt[s.ID]; !ok {
				p.endedAt[s.ID] = now
				if s.Phase == "Failed" {
					p.Log.Printf("session %s: %s's %s stopped with an error: %s", s.ID, s.User, s.Scenario, cmp.Or(s.Error, "no message"))
				}
			}
			// Long enough for the page to show the result.
			if now.Sub(p.endedAt[s.ID]) > time.Minute {
				reap = append(reap, s.ID)
			}
		case p.MaxAge > 0 && now.Sub(s.Created) > p.MaxAge:
			reap = append(reap, s.ID)
		}
	}
	p.listed = true
	p.mu.Unlock()
	for _, id := range reap {
		p.Log.Printf("removing session %s", id)
		if err := p.Backend.Delete(ctx, id); err != nil {
			p.Log.Printf("removing session %s: %v", id, err)
		}
	}
}

// Handler serves the portal.
func (p *Portal) Handler() http.Handler {
	page, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.Handle("GET /", http.FileServerFS(page))
	mux.Handle("GET /ui/", http.StripPrefix("/ui", webui.Static()))
	mux.HandleFunc("GET /api/me", p.getMe)
	mux.HandleFunc("POST /api/me", p.setMe)
	mux.HandleFunc("GET /api/scenarios", p.withUser(p.listScenarios))
	mux.HandleFunc("GET /api/scoreboard", p.scoreboard)
	mux.HandleFunc("GET /api/session", p.withUser(p.mySession))
	mux.HandleFunc("POST /api/sessions", p.withUser(p.createSession))
	mux.HandleFunc("DELETE /api/sessions/{id}", p.withUser(p.deleteSession))
	mux.HandleFunc("GET /s/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/s/"+r.PathValue("id")+"/", http.StatusFound)
	})
	mux.HandleFunc("GET /s/{id}/{rest...}", p.withUser(p.sessionPage))
	mux.HandleFunc("POST /s/{id}/{rest...}", p.withUser(p.sessionPage))
	mux.HandleFunc("POST /internal/results", p.saveResult)
	return sameOrigin(mux)
}

// sameOrigin refuses requests that change something when a browser sent
// them from another site. The session pages it proxies open root shells.
func sameOrigin(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && (r.Method != http.MethodGet || r.Header.Get("Upgrade") != "") {
			u, err := url.Parse(o)
			if err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

// ---- Learners ----

const userCookie = "opsschool_user"

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+-]{0,63}$`)

// user returns who is asking, or "".
func (p *Portal) user(r *http.Request) string {
	if p.UserHeader != "" {
		return strings.TrimSpace(r.Header.Get(p.UserHeader))
	}
	c, err := r.Cookie(userCookie)
	if err != nil || !validName.MatchString(c.Value) {
		return ""
	}
	return c.Value
}

func (p *Portal) withUser(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := p.user(r)
		if u == "" {
			http.Error(w, "tell us your name first", http.StatusUnauthorized)
			return
		}
		h(w, r, u)
	}
}

func (p *Portal) getMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"user": p.user(r), "from_proxy": p.UserHeader != ""})
}

func (p *Portal) setMe(w http.ResponseWriter, r *http.Request) {
	if p.UserHeader != "" {
		http.Error(w, "your name comes from your sign-in", http.StatusConflict)
		return
	}
	var body struct{ User string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body.User = strings.TrimSpace(body.User)
	if !validName.MatchString(body.User) {
		http.Error(w, "use letters, numbers, dots, dashes or an email address, up to 64 characters", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: userCookie, Value: body.User, Path: "/", MaxAge: 365 * 24 * 3600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, map[string]any{"user": body.User})
}

// ---- Scenarios and the scoreboard ----

// ScenarioRow is a scenario in the portal's list, with the learner's best.
type ScenarioRow struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Level     int    `json:"level"`
	TimeLimit int64  `json:"time_limit"` // seconds
	Best      *int   `json:"best,omitempty"`
	Hint      bool   `json:"hint,omitempty"`
	Fixed     int64  `json:"fixed,omitempty"`     // fastest fix, seconds
	Mitigated int64  `json:"mitigated,omitempty"` // that run's mitigation
}

func (p *Portal) listScenarios(w http.ResponseWriter, r *http.Request, user string) {
	rs, err := p.Store.All()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	best, fastest := results.Best(rs, user), results.Fastest(rs, user)
	rows := []ScenarioRow{}
	for _, s := range p.Scenarios {
		id := s.Spec.ID
		row := ScenarioRow{ID: id, Category: strings.SplitN(id, "/", 2)[0], Level: s.Spec.Level,
			TimeLimit: int64(s.Spec.TimeLimit.Seconds())}
		if b, ok := best[id]; ok {
			row.Best, row.Hint = &b.Score, b.HintsUsed > 0
		}
		if f, ok := fastest[id]; ok {
			row.Fixed, row.Mitigated = int64(f.TierPassed[results.TierFixed]), int64(f.TierPassed[results.TierMitigated])
		}
		rows = append(rows, row)
	}
	writeJSON(w, rows)
}

// Scoreboard is everyone's results.
type Scoreboard struct {
	Learners  []LearnerRow `json:"learners"`
	Scenarios []FastRow    `json:"scenarios"`
}

// LearnerRow totals a learner's best result on each scenario.
type LearnerRow struct {
	User   string `json:"user"`
	Points int    `json:"points"`
	Fixed  int    `json:"fixed"`  // scenarios fixed
	Played int    `json:"played"` // scenarios with a result
}

// FastRow is a scenario's fastest fix by anyone.
type FastRow struct {
	ID        string `json:"id"`
	User      string `json:"user,omitempty"`
	Fixed     int64  `json:"fixed,omitempty"`
	Mitigated int64  `json:"mitigated,omitempty"`
}

// BuildScoreboard totals results.
func BuildScoreboard(rs []results.Result, scs []*scenario.Scenario) Scoreboard {
	sb := Scoreboard{Learners: []LearnerRow{}, Scenarios: []FastRow{}}
	users := map[string]bool{}
	for _, r := range rs {
		users[r.User] = true
	}
	for u := range users {
		row := LearnerRow{User: u}
		for _, b := range results.Best(rs, u) {
			row.Points += b.Score
			row.Played++
			if b.Passed(results.TierFixed) {
				row.Fixed++
			}
		}
		sb.Learners = append(sb.Learners, row)
	}
	sort.Slice(sb.Learners, func(i, j int) bool {
		a, b := sb.Learners[i], sb.Learners[j]
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		return a.User < b.User
	})
	fastest := results.Fastest(rs, "")
	for _, s := range scs {
		row := FastRow{ID: s.Spec.ID}
		if f, ok := fastest[s.Spec.ID]; ok {
			row.User = f.User
			row.Fixed, row.Mitigated = int64(f.TierPassed[results.TierFixed]), int64(f.TierPassed[results.TierMitigated])
		}
		sb.Scenarios = append(sb.Scenarios, row)
	}
	return sb
}

func (p *Portal) scoreboard(w http.ResponseWriter, r *http.Request) {
	rs, err := p.Store.All()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, BuildScoreboard(rs, p.Scenarios))
}

// ---- Sessions ----

// mine returns the user's session that hasn't ended, if any.
func (p *Portal) mine(user string) (Session, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.sessions {
		if s.User == user && !s.Ended() {
			return s, true
		}
	}
	return Session{}, false
}

func (p *Portal) mySession(w http.ResponseWriter, r *http.Request, user string) {
	s, ok := p.mine(user)
	if !ok {
		writeJSON(w, nil)
		return
	}
	writeJSON(w, s)
}

func (p *Portal) createSession(w http.ResponseWriter, r *http.Request, user string) {
	var body struct{ Scenario string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if p.find(body.Scenario) == nil {
		http.Error(w, "no such scenario", http.StatusNotFound)
		return
	}
	p.mu.Lock()
	if !p.listed {
		p.mu.Unlock()
		http.Error(w, "the portal is still starting; try again in a few seconds", http.StatusServiceUnavailable)
		return
	}
	running := 0
	for _, s := range p.sessions {
		if !s.Ended() {
			running++
		}
		if s.User == user && !s.Ended() {
			p.mu.Unlock()
			http.Error(w, "you already have a session running; end it first", http.StatusConflict)
			return
		}
	}
	if p.creating[user] {
		p.mu.Unlock()
		http.Error(w, "your session is already being created", http.StatusConflict)
		return
	}
	if p.MaxSessions > 0 && running >= p.MaxSessions {
		p.mu.Unlock()
		http.Error(w, "every scenario machine is in use right now; try again in a few minutes", http.StatusServiceUnavailable)
		return
	}
	if p.creating == nil {
		p.creating = map[string]bool{}
	}
	p.creating[user] = true
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.creating, user); p.mu.Unlock() }()

	id := randomHex(4)
	s := Session{
		ID: id, User: user, Scenario: body.Scenario, Created: time.Now(), Phase: "Pending",
		Token: randomHex(16), GrafanaRoot: p.publicURL(r) + "/s/" + id + "/grafana/",
	}
	if err := p.Backend.Create(r.Context(), s); err != nil {
		p.Log.Printf("creating session for %s: %v", user, err)
		http.Error(w, "couldn't create the session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	p.Log.Printf("session %s: %s started %s", id, user, body.Scenario)
	p.mu.Lock()
	p.sessions[id] = s
	p.mu.Unlock()
	writeJSON(w, s)
}

func (p *Portal) deleteSession(w http.ResponseWriter, r *http.Request, user string) {
	s, ok := p.get(r.PathValue("id"), user)
	if !ok {
		http.Error(w, errNoSession.Error(), http.StatusNotFound)
		return
	}
	if err := p.Backend.Delete(r.Context(), s.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.Log.Printf("session %s: %s discarded it", s.ID, user)
	p.mu.Lock()
	s.Phase = "Failed"
	p.sessions[s.ID] = s
	p.mu.Unlock()
	writeJSON(w, s)
}

// get returns a session that belongs to user.
func (p *Portal) get(id, user string) (Session, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[id]
	return s, ok && s.User == user
}

func (p *Portal) find(id string) *scenario.Scenario {
	for _, s := range p.Scenarios {
		if s.Spec.ID == id {
			return s
		}
	}
	return nil
}

func (p *Portal) publicURL(r *http.Request) string {
	if p.PublicURL != "" {
		return strings.TrimSuffix(p.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// sessionAPI lists the session page's calls to its runner. Anything else
// under /s/<id>/ is the page's own files, which the portal serves itself so
// the page loads while the session is still starting.
func sessionAPI(rest string) bool {
	switch rest {
	case "status", "hint", "quiz", "verify", "stop":
		return true
	}
	return strings.HasPrefix(rest, "api/")
}

var proxyTransport = &http.Transport{
	DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	MaxIdleConnsPerHost: 8,
	IdleConnTimeout:     90 * time.Second,
}

func (p *Portal) sessionPage(w http.ResponseWriter, r *http.Request, user string) {
	s, ok := p.get(r.PathValue("id"), user)
	if !ok {
		if r.PathValue("rest") == "status" {
			writeStarting(w, Starting{Error: "This session isn't yours, or it has ended and been removed."})
			return
		}
		http.Error(w, errNoSession.Error(), http.StatusNotFound)
		return
	}
	rest := r.PathValue("rest")
	switch {
	case rest == "grafana" || strings.HasPrefix(rest, "grafana/"):
		if s.Addr == "" {
			http.Error(w, "the session is still starting", http.StatusServiceUnavailable)
			return
		}
		// Grafana serves from this path itself.
		p.proxy(w, r, s, telemetry.GrafanaPort, r.URL.Path)
	case sessionAPI(rest):
		if s.Phase == "Failed" {
			writeStarting(w, Starting{Error: "Something went wrong on our side and this session stopped. " +
				"Please start it again from the scenario list. If it keeps happening, tell whoever runs Ops School here."})
			return
		}
		if s.Ended() {
			writeStarting(w, Starting{Error: "This session has ended."})
			return
		}
		if s.Addr == "" || s.Phase != "Running" {
			if rest == "status" {
				writeStarting(w, Starting{Starting: true, Message: pendingMessage(s)})
				return
			}
			http.Error(w, "the session is still starting", http.StatusServiceUnavailable)
			return
		}
		p.proxy(w, r, s, RunnerPort, "/"+rest)
	default:
		http.StripPrefix("/s/"+s.ID, webui.Static()).ServeHTTP(w, r)
	}
}

func pendingMessage(s Session) string {
	switch {
	case strings.Contains(s.Waiting, "ImagePull") || s.Waiting == "ErrImagePull" || s.Waiting == "InvalidImageName":
		return "Having trouble downloading the session's software. If this lasts, tell whoever runs Ops School here."
	case s.Addr == "" && s.Waiting == "":
		return "Waiting for room on a server"
	default:
		return "Downloading and starting the session's software. The first session on a server takes a few minutes."
	}
}

func (p *Portal) proxy(w http.ResponseWriter, r *http.Request, s Session, port int, path string) {
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort(s.Addr, fmt.Sprint(port))}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path, pr.Out.URL.RawPath = path, ""
			// The terminal's WebSocket checks that Origin matches Host;
			// sameOrigin has checked Origin against this Host already.
			pr.Out.Host = pr.In.Host
			pr.Out.Header.Set(session.TokenHeader, s.Token)
			pr.SetXForwarded()
		},
		Transport:     cmp.Or(p.transport, http.RoundTripper(proxyTransport)),
		FlushInterval: -1, // verify streams its progress
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if strings.HasSuffix(path, "/status") {
				writeStarting(w, Starting{Starting: true, Message: "Starting your session"})
				return
			}
			http.Error(w, "the session isn't answering", http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}

// saveResult records a result that a session runner sends when its
// session ends.
func (p *Portal) saveResult(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get(session.TokenHeader)
	var res results.Result
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	var s Session
	found := false
	for _, c := range p.sessions {
		if token != "" && subtle.ConstantTimeCompare([]byte(c.Token), []byte(token)) == 1 {
			s, found = c, true
		}
	}
	if !found {
		p.mu.Unlock()
		http.Error(w, "unknown session", http.StatusForbidden)
		return
	}
	if p.recorded == nil {
		p.recorded = map[string]bool{}
	}
	if p.recorded[s.ID] {
		p.mu.Unlock()
		writeJSON(w, res)
		return
	}
	// The runner reports what happened; who and what come from the portal.
	res.User, res.Scenario = s.User, s.Scenario
	err := p.Store.Append(res)
	if err == nil {
		p.recorded[s.ID] = true
	}
	p.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.Log.Printf("session %s: %s scored %d on %s", s.ID, s.User, res.Score, s.Scenario)
	writeJSON(w, res)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// ErrNoScenarios is returned when the portal finds nothing to offer.
var ErrNoScenarios = errors.New("no valid scenarios found")
