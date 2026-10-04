package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLocalOnly(t *testing.T) {
	h := LocalOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, c := range []struct {
		host, origin string
		want         int
	}{
		{"127.0.0.1:19999", "", http.StatusOK},                       // the CLI
		{"127.0.0.1:19999", "http://127.0.0.1:19999", http.StatusOK}, // the page
		{"localhost:19999", "http://localhost:19999", http.StatusOK},
		{"[::1]:19999", "", http.StatusOK},
		{"127.0.0.1:19999", "https://evil.example", http.StatusForbidden},         // another site
		{"evil.example:19999", "http://evil.example:19999", http.StatusForbidden}, // DNS rebinding
		{"172.17.0.1:19999", "", http.StatusForbidden},                            // the Docker network
	} {
		r := httptest.NewRequest(http.MethodPost, "/stop", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("host %q origin %q: got %d, want %d", c.host, c.origin, w.Code, c.want)
		}
	}
}

func TestPageServed(t *testing.T) {
	srv := httptest.NewServer(Handler(Config{}))
	defer srv.Close()
	for _, path := range []string{"/", "/app.js", "/app.css", "/vendor/xterm.mjs", "/fonts/AtkinsonHyperlegibleNext.woff2"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}
}

func TestCharts(t *testing.T) {
	var queries []string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		queries = append(queries, q)
		if strings.Contains(q, "node_filesystem") {
			w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
				{"metric":{"mountpoint":"/"},"values":[[1000,"40"],[1015,"41"]]},
				{"metric":{"mountpoint":"/data"},"values":[[1000,"99.5"],[1015,"NaN"]]}]}}`))
			return
		}
		w.Write([]byte(`{"status":"error","error":"bad query"}`))
	}))
	defer prom.Close()
	srv := httptest.NewServer(Handler(Config{PrometheusURL: prom.URL}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/charts?minutes=5")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ Charts []chartData }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Charts) != len(charts) || len(queries) != len(charts) {
		t.Fatalf("got %d charts from %d queries", len(body.Charts), len(queries))
	}
	for _, c := range body.Charts {
		if c.ID != "disk" {
			if c.Error == "" {
				t.Errorf("%s: a failed query should report an error", c.ID)
			}
			continue
		}
		if len(c.Series) != 2 || c.Series[1].Label != "/data" {
			t.Fatalf("disk series: %+v", c.Series)
		}
		if n := len(c.Series[1].Points); n != 1 {
			t.Errorf("NaN samples should be dropped, got %d points", n)
		}
	}
}

func TestTerminal(t *testing.T) {
	srv := httptest.NewServer(Handler(Config{
		Shell: func() []string { return []string{"sh", "-c", `stty size; read line; echo "got $line"`} },
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/terminal", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	var out strings.Builder
	readUntil := func(want string) {
		for !strings.Contains(out.String(), want) {
			_, b, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("waiting for %q, got %q: %v", want, out.String(), err)
			}
			out.Write(b)
		}
	}
	readUntil("24 80") // the default size, before the page sends its own
	conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`))
	conn.Write(ctx, websocket.MessageBinary, []byte("hello\r"))
	readUntil("got hello")
	// The shell has exited; the server closes the connection.
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				t.Errorf("close: %v", err)
			}
			break
		}
	}
}

func TestTerminalRefusesOtherSites(t *testing.T) {
	srv := httptest.NewServer(Handler(Config{Shell: func() []string { return []string{"true"} }}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/terminal", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://evil.example"}},
	})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin terminal: %v %v", resp, err)
	}
}
