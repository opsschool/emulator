// Package telemetryfs holds the telemetry stack's config templates. The CLI
// renders them into the session directory; see internal/telemetry.
package telemetryfs

import "embed"

// FS holds every template in this directory.
//
//go:embed docker-compose.yml.tmpl prometheus.yml.tmpl loki.yaml grafana
var FS embed.FS
