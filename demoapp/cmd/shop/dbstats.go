package main

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"
)

// dbStats exports connection pool stats for one database handle, labelled
// db="source" or db="replica".
func dbStats(db *sql.DB, name string) prometheus.Collector {
	return &poolCollector{db: db, labels: prometheus.Labels{"db": name}}
}

type poolCollector struct {
	db     *sql.DB
	labels prometheus.Labels
}

func (c *poolCollector) desc(name, help string) *prometheus.Desc {
	return prometheus.NewDesc("shop_db_pool_"+name, help, nil, c.labels)
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(c, ch)
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.db.Stats()
	ch <- prometheus.MustNewConstMetric(c.desc("max_open", "Configured maximum open connections."), prometheus.GaugeValue, float64(s.MaxOpenConnections))
	ch <- prometheus.MustNewConstMetric(c.desc("open", "Open connections."), prometheus.GaugeValue, float64(s.OpenConnections))
	ch <- prometheus.MustNewConstMetric(c.desc("in_use", "Connections in use."), prometheus.GaugeValue, float64(s.InUse))
	ch <- prometheus.MustNewConstMetric(c.desc("idle", "Idle connections."), prometheus.GaugeValue, float64(s.Idle))
	ch <- prometheus.MustNewConstMetric(c.desc("wait_count_total", "Times a request waited for a connection."), prometheus.CounterValue, float64(s.WaitCount))
	ch <- prometheus.MustNewConstMetric(c.desc("wait_seconds_total", "Total time spent waiting for a connection."), prometheus.CounterValue, s.WaitDuration.Seconds())
}
