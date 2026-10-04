package edge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/opsschool/emulator/internal/loadgen"
)

func TestStatus(t *testing.T) {
	for _, c := range []struct {
		name string
		r    loadgen.Result
		want int
	}{
		{"response", loadgen.Result{Code: 201}, 201},
		{"timeout", loadgen.Result{Err: fmt.Errorf("get: %w", context.DeadlineExceeded)}, 504},
		{"refused", loadgen.Result{Err: fmt.Errorf("dial: %w", syscall.ECONNREFUSED)}, 502},
		{"reset", loadgen.Result{Err: fmt.Errorf("read: %w", syscall.ECONNRESET)}, 502},
		{"other", loadgen.Result{Err: io.EOF}, 502},
	} {
		if got, _ := Status(c.r); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
}

func TestObserveShipsLog(t *testing.T) {
	got := make(chan string, 1)
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var push struct {
			Streams []struct {
				Stream map[string]string
				Values [][2]string
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&push); err != nil || len(push.Streams) != 1 {
			t.Errorf("bad push: %v", err)
			return
		}
		s := push.Streams[0]
		got <- s.Stream["job"] + " " + s.Values[0][1]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer loki.Close()

	e := New(loki.URL)
	e.Observe(loadgen.Result{Method: "POST", Path: "/orders", Route: "POST /orders", Customer: 7,
		Err: context.DeadlineExceeded, Duration: 10 * time.Second})
	if n := count(t, e, "504"); n != 1 {
		t.Errorf("edge_requests_total{code=504} = %v, want 1", n)
	}
	e.flush(context.Background())
	select {
	case line := <-got:
		for _, want := range []string{"edge ", `"POST /orders HTTP/1.1" 504`, "rt=10.000", "upstream timed out"} {
			if !strings.Contains(line, want) {
				t.Errorf("log line %q lacks %q", line, want)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("no push")
	}
}

func TestFlushKeepsLinesWhenLokiIsDown(t *testing.T) {
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer loki.Close()
	e := New(loki.URL)
	e.Observe(loadgen.Result{Method: "GET", Path: "/products", Route: "GET /products", Code: 200})
	e.flush(context.Background())
	if len(e.lines) != 1 {
		t.Errorf("%d lines buffered after a failed push, want 1", len(e.lines))
	}
}

func count(t *testing.T, e *Edge, code string) float64 {
	t.Helper()
	mfs, err := e.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	total := 0.0
	for _, mf := range mfs {
		if mf.GetName() != "edge_requests_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "code" && l.GetValue() == code {
					total += m.GetCounter().GetValue()
				}
			}
		}
	}
	return total
}
