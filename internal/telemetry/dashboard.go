package telemetry

import (
	"encoding/json"
	"fmt"
)

// Panel is a Grafana panel, kept as a map so scenario panels pass through
// unchanged.
type Panel = map[string]any

type builder struct {
	panels []Panel
	y      int // next free row
	x      int // next free column in the current row
	rowH   int
	id     int
}

func (b *builder) nextID() int { b.id++; return b.id }

func (b *builder) row(title string) {
	b.newline()
	b.panels = append(b.panels, Panel{
		"type": "row", "id": b.nextID(), "title": title, "collapsed": false,
		"gridPos": map[string]int{"h": 1, "w": 24, "x": 0, "y": b.y},
	})
	b.y++
}

func (b *builder) newline() {
	if b.x > 0 {
		b.y += b.rowH
		b.x, b.rowH = 0, 0
	}
}

func (b *builder) place(p Panel, w, h int) {
	if b.x+w > 24 {
		b.newline()
	}
	p["id"] = b.nextID()
	p["gridPos"] = map[string]int{"h": h, "w": w, "x": b.x, "y": b.y}
	b.x += w
	b.rowH = max(b.rowH, h)
	b.panels = append(b.panels, p)
}

var promDS = map[string]string{"type": "prometheus", "uid": "prometheus"}
var lokiDS = map[string]string{"type": "loki", "uid": "loki"}

type query struct{ expr, legend string }

func timeseries(title, unit string, qs ...query) Panel {
	targets := make([]map[string]any, len(qs))
	for i, q := range qs {
		targets[i] = map[string]any{"refId": string(rune('A' + i)), "expr": q.expr, "legendFormat": q.legend, "datasource": promDS}
	}
	defaults := map[string]any{"unit": unit, "min": 0, "custom": map[string]any{"fillOpacity": 10, "showPoints": "never"}}
	if unit == "percentunit" {
		defaults["max"] = 1
	}
	return Panel{
		"type": "timeseries", "title": title, "datasource": promDS, "targets": targets,
		"fieldConfig": map[string]any{"defaults": defaults, "overrides": []any{}},
		"options":     map[string]any{"legend": map[string]any{"displayMode": "list", "placement": "bottom"}, "tooltip": map[string]any{"mode": "multi"}},
	}
}

func stat(title, expr, unit string, mappings []any, thresholds []any) Panel {
	defaults := map[string]any{"unit": unit, "noValue": "waiting"}
	if mappings != nil {
		defaults["mappings"] = mappings
	}
	if thresholds != nil {
		defaults["thresholds"] = map[string]any{"mode": "absolute", "steps": thresholds}
		defaults["color"] = map[string]any{"mode": "thresholds"}
	}
	colorMode := "none" // plain numbers; Grafana's default thresholds turn them red
	if thresholds != nil {
		colorMode = "background"
	}
	return Panel{
		"type": "stat", "title": title, "datasource": promDS,
		"targets":     []map[string]any{{"refId": "A", "expr": expr, "datasource": promDS, "instant": true}},
		"fieldConfig": map[string]any{"defaults": defaults, "overrides": []any{}},
		"options":     map[string]any{"colorMode": colorMode, "graphMode": "none", "reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}}},
	}
}

func logs(title, expr string) Panel {
	return Panel{
		"type": "logs", "title": title, "datasource": lokiDS,
		"targets": []map[string]any{{"refId": "A", "expr": expr, "datasource": lokiDS}},
		"options": map[string]any{"showTime": true, "wrapLogMessage": true, "sortOrder": "Descending", "enableLogDetails": true},
	}
}

var passFail = []any{
	map[string]any{"type": "value", "options": map[string]any{
		"0": map[string]any{"text": "not yet", "color": "orange"},
		"1": map[string]any{"text": "passed", "color": "green"},
	}},
}

var passThresholds = []any{
	map[string]any{"color": "orange", "value": nil},
	map[string]any{"color": "green", "value": 1},
}

// Dashboard builds the scenario dashboard: the base rows every scenario gets,
// then the scenario's own panels, if any. extra is the scenario's
// dashboard.json: {"panels": [...]}.
func Dashboard(scenarioID string, extra []byte) ([]byte, error) {
	b := &builder{}

	b.row("Tier status")
	b.place(stat("Mitigated", fmt.Sprintf(`max(opsschool_check_passed{scenario=%q,tier="mitigated"})`, scenarioID), "none", passFail, passThresholds), 6, 4)
	b.place(stat("Fixed", fmt.Sprintf(`max(opsschool_check_passed{scenario=%q,tier="fixed"})`, scenarioID), "none", passFail, passThresholds), 6, 4)
	b.place(stat("Elapsed", `max(opsschool_session_elapsed_seconds)`, "s", nil, nil), 6, 4)
	b.place(stat("Hints used", `max(opsschool_hints_used)`, "none", nil, nil), 6, 4)

	b.row("Service (RED)")
	b.place(timeseries("Request rate by route", "reqps",
		query{`sum by (route) (rate(http_requests_total{job="shop"}[1m]))`, "{{route}}"}), 8, 8)
	b.place(timeseries("Error rate (5xx) by route", "percentunit",
		query{`(sum by (route) (rate(http_requests_total{job="shop",code=~"5.."}[1m])) or 0 * sum by (route) (rate(http_requests_total{job="shop"}[1m]))) / sum by (route) (rate(http_requests_total{job="shop"}[1m]))`, "{{route}}"}), 8, 8)
	b.place(timeseries("Latency", "s",
		query{`histogram_quantile(0.5, sum by (le) (rate(http_request_duration_seconds_bucket{job="shop"}[1m])))`, "p50"},
		query{`histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{job="shop"}[1m])))`, "p95"},
		query{`histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket{job="shop"}[1m])))`, "p99"}), 8, 8)
	b.place(timeseries("p99 latency by route", "s",
		query{`histogram_quantile(0.99, sum by (le, route) (rate(http_request_duration_seconds_bucket{job="shop"}[1m])))`, "{{route}}"}), 8, 8)
	b.place(timeseries("DB connection pool", "short",
		query{`shop_db_pool_in_use{job="shop"}`, "in use ({{db}})"},
		query{`shop_db_pool_max_open{job="shop"}`, "max ({{db}})"},
		query{`rate(shop_db_pool_wait_count_total{job="shop"}[1m])`, "waits/s ({{db}})"}), 8, 8)
	b.place(timeseries("Worker queue", "short",
		query{`shop_worker_queue_depth{job="shop-worker"}`, "queue depth"},
		query{`rate(shop_worker_orders_processed_total{job="shop-worker"}[1m])`, "processed/s"}), 8, 8)

	b.row("Host (USE)")
	b.place(timeseries("CPU", "percentunit",
		query{`sum by (mode) (rate(node_cpu_seconds_total{mode!="idle"}[1m])) / scalar(count(node_cpu_seconds_total{mode="idle"}))`, "{{mode}}"}), 8, 8)
	b.place(timeseries("Memory", "bytes",
		query{`node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes`, "used"},
		query{`node_memory_MemAvailable_bytes`, "available"},
		query{`node_memory_SwapTotal_bytes - node_memory_SwapFree_bytes`, "swap used"}), 8, 8)
	b.place(timeseries("Swap activity", "short",
		query{`rate(node_vmstat_pswpin[1m])`, "pages in/s"},
		query{`rate(node_vmstat_pswpout[1m])`, "pages out/s"}), 8, 8)
	b.place(timeseries("Disk space used", "percentunit",
		query{`1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay"}`, "{{mountpoint}}"}), 8, 8)
	b.place(timeseries("Inodes used", "percentunit",
		query{`1 - node_filesystem_files_free{fstype!~"tmpfs|overlay"} / node_filesystem_files{fstype!~"tmpfs|overlay"}`, "{{mountpoint}}"}), 8, 8)
	b.place(timeseries("Disk I/O", "Bps",
		query{`sum by (device) (rate(node_disk_read_bytes_total[1m]))`, "read {{device}}"},
		query{`sum by (device) (rate(node_disk_written_bytes_total[1m]))`, "write {{device}}"}), 8, 8)
	b.place(timeseries("I/O wait and disk utilization", "percentunit",
		query{`sum(rate(node_cpu_seconds_total{mode="iowait"}[1m])) / scalar(count(node_cpu_seconds_total{mode="idle"}))`, "iowait"},
		query{`rate(node_disk_io_time_seconds_total[1m])`, "busy {{device}}"}), 8, 8)
	b.place(timeseries("Network", "Bps",
		query{`sum by (device) (rate(node_network_receive_bytes_total{device!="lo"}[1m]))`, "rx {{device}}"},
		query{`sum by (device) (rate(node_network_transmit_bytes_total{device!="lo"}[1m]))`, "tx {{device}}"}), 8, 8)
	b.place(timeseries("Load", "short",
		query{`node_load1`, "1m"}, query{`node_load5`, "5m"}, query{`node_load15`, "15m"}), 8, 8)

	b.row("MySQL")
	b.place(timeseries("Connections", "short",
		query{`mysql_global_status_threads_connected`, "connected"},
		query{`mysql_global_status_threads_running`, "running"},
		query{`mysql_global_variables_max_connections`, "max"}), 8, 8)
	b.place(timeseries("Queries per second", "qps",
		query{`rate(mysql_global_status_queries[1m])`, "queries"},
		query{`rate(mysql_global_status_slow_queries[1m])`, "slow"}), 8, 8)
	b.place(timeseries("InnoDB row lock waits", "short",
		query{`rate(mysql_global_status_innodb_row_lock_waits[1m])`, "waits/s"},
		query{`mysql_global_status_innodb_row_lock_current_waits`, "current"}), 8, 8)
	b.place(timeseries("Replica lag", "s",
		query{`mysql_slave_status_seconds_behind_source or mysql_slave_status_seconds_behind_master`, "{{instance}}"}), 8, 8)
	b.place(timeseries("Connection errors", "short",
		query{`rate(mysql_global_status_connection_errors_total[1m])`, "{{error}}"}), 8, 8)

	b.row("Redis")
	b.place(timeseries("Memory", "bytes", query{`redis_memory_used_bytes`, "used"}, query{`redis_memory_max_bytes`, "max"}), 8, 8)
	b.place(timeseries("Cache hit rate", "percentunit",
		query{`sum(rate(shop_cache_requests_total{result="hit"}[1m])) / sum(rate(shop_cache_requests_total[1m]))`, "hit rate"}), 8, 8)
	b.place(timeseries("Connected clients", "short", query{`redis_connected_clients`, "clients"}), 8, 8)

	b.row("Logs")
	b.place(logs("Shop logs", `{job="shop"}`), 24, 10)
	b.place(logs("System journal", `{job="journal"}`), 24, 10)

	if len(extra) > 0 {
		var e struct {
			Panels []Panel `json:"panels"`
		}
		if err := json.Unmarshal(extra, &e); err != nil {
			return nil, fmt.Errorf("scenario dashboard.json: %w", err)
		}
		if len(e.Panels) > 0 {
			b.row("Scenario")
			for _, p := range e.Panels {
				w, h := 12, 8
				if gp, ok := p["gridPos"].(map[string]any); ok {
					if v, ok := gp["w"].(float64); ok {
						w = int(v)
					}
					if v, ok := gp["h"].(float64); ok {
						h = int(v)
					}
				}
				b.place(p, w, h)
			}
		}
	}

	return json.MarshalIndent(map[string]any{
		"uid":           "scenario",
		"title":         "Ops School: " + scenarioID,
		"editable":      true,
		"refresh":       "5s",
		"time":          map[string]string{"from": "now-15m", "to": "now"},
		"timepicker":    map[string]any{"refresh_intervals": []string{"5s", "10s", "30s", "1m"}},
		"schemaVersion": 39,
		"tags":          []string{"opsschool"},
		"panels":        b.panels,
	}, "", "  ")
}
