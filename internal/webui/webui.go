// Package webui serves the session page: the incident, progress and hints,
// a terminal on the scenario machine, and a few dashboard charts. The
// session daemon mounts it next to its control API, which the page calls.
package webui

import (
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
)

//go:embed static
var static embed.FS

// Config is what the page needs from the session.
type Config struct {
	// Shell returns the command for an interactive root shell on the
	// scenario machine, as `opsschool shell` runs it.
	Shell func() []string
	// PrometheusURL is queried for the dashboard charts.
	PrometheusURL string
	// Architecture is the image's architecture.yaml, for the Architecture
	// tab. Optional.
	Architecture []byte
	// Changes returns the Recent changes tab's list, newest first.
	// Optional.
	Changes func() any
	Log     *log.Logger
}

// Handler serves the page, its files, the terminal and the charts.
func Handler(c Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", Static())
	mux.HandleFunc("GET /api/terminal", c.terminal)
	mux.HandleFunc("GET /api/charts", c.charts)
	mux.HandleFunc("GET /api/architecture", c.architecture)
	mux.HandleFunc("GET /api/changes", c.changes)
	return mux
}

// Static serves the page and its files, without the session API. The
// hosted portal serves them itself while a session starts.
func Static() http.Handler {
	files, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServerFS(files)
}

// LocalOnly rejects requests that a browser sent from another site, and
// requests addressed to a name other than the loopback address. The
// session page opens a root shell, so a web page the learner happens to
// visit must not be able to reach it, by a cross-site request or by DNS
// rebinding. Command-line clients send no Origin header and pass.
func LocalOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "the session page only answers on 127.0.0.1", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
