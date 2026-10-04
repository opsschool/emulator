package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// chart is one of the page's dashboard charts. The queries live here, not
// in the page, so the page cannot be used to run arbitrary queries.
type chart struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Help  string `json:"help"`
	// Unit is how the page formats values: "percent", "seconds" or "".
	Unit string `json:"unit"`
	// Axis names what the vertical axis measures.
	Axis string `json:"axis"`
	// Label names the series label to show in the legend, for charts
	// with more than one line.
	Label string `json:"-"`
	Query string `json:"-"`
}

// Real filesystems only: the scenario machine also has tmpfs and overlay
// mounts that would crowd the chart.
const realFS = `fstype!~"tmpfs|devtmpfs|overlay|squashfs|ramfs|nsfs|autofs"`

var charts = []chart{
	{
		ID: "errors", Title: "Errors customers see", Unit: "percent", Axis: "% of requests failed",
		Help:  "Share of requests that failed at the load balancer",
		Query: `100 * (sum(rate(edge_requests_total{code=~"5.."}[1m])) or vector(0)) / sum(rate(edge_requests_total[1m]))`,
	},
	{
		ID: "orders", Title: "Orders per minute", Axis: "orders per minute",
		Help:  "Orders customers placed successfully",
		Query: `60 * (sum(rate(edge_requests_total{route="POST /orders",code=~"2.."}[1m])) or vector(0))`,
	},
	{
		ID: "latency", Title: "Slowest 1% of requests", Unit: "seconds", Axis: "p99 response time",
		Help:  "p99 latency at the load balancer",
		Query: `histogram_quantile(0.99, sum by (le) (rate(edge_request_duration_seconds_bucket[1m])))`,
	},
	{
		ID: "disk", Title: "Disk space used", Unit: "percent", Label: "mountpoint", Axis: "% of disk used",
		Help:  "Each filesystem on the server",
		Query: `100 * (1 - node_filesystem_avail_bytes{` + realFS + `} / node_filesystem_size_bytes{` + realFS + `})`,
	},
}

type series struct {
	Label  string       `json:"label"`
	Points [][2]float64 `json:"points"` // unix seconds, value
}

type chartData struct {
	chart
	Series []series `json:"series"`
	Error  string   `json:"error,omitempty"`
}

// charts answers GET /api/charts?minutes=15 with each chart's series.
func (c Config) charts(w http.ResponseWriter, r *http.Request) {
	minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
	if minutes <= 0 || minutes > 120 {
		minutes = 15
	}
	end := time.Now()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	step := time.Duration(minutes) * time.Minute / 120

	out := make([]chartData, len(charts))
	var wg sync.WaitGroup
	for i, ch := range charts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = chartData{chart: ch}
			s, err := c.queryRange(r.Context(), ch, start, end, step)
			if err != nil {
				out[i].Error = "no data yet"
				c.logf("chart %s: %v", ch.ID, err)
				return
			}
			out[i].Series = s
		}()
	}
	wg.Wait()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"charts": out})
}

func (c Config) queryRange(ctx context.Context, ch chart, start, end time.Time, step time.Duration) ([]series, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	q := url.Values{
		"query": {ch.Query},
		"start": {strconv.FormatInt(start.Unix(), 10)},
		"end":   {strconv.FormatInt(end.Unix(), 10)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', 0, 64)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.PrometheusURL+"/api/v1/query_range?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]any          `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("prometheus: %s", body.Error)
	}
	var out []series
	for _, res := range body.Data.Result {
		s := series{Label: res.Metric[ch.Label]}
		for _, v := range res.Values {
			t, _ := v[0].(float64)
			str, _ := v[1].(string)
			f, err := strconv.ParseFloat(str, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) { // no traffic in that step
				continue
			}
			s.Points = append(s.Points, [2]float64{t, f})
		}
		out = append(out, s)
	}
	return out, nil
}
