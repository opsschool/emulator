package webui

import (
	"encoding/json"
	"net/http"

	"gopkg.in/yaml.v3"
)

// Architecture is an image's architecture.yaml: the parts of the system,
// grouped by where they run.
type Architecture struct {
	Zones []Zone `yaml:"zones" json:"zones"`
}

// Zone is a place things run, such as the server or the edge.
type Zone struct {
	ID         string      `yaml:"id" json:"id"`
	Name       string      `yaml:"name" json:"name"`
	About      string      `yaml:"about" json:"about"`
	Components []Component `yaml:"components" json:"components"`
}

// Component is one part of the system, and where to look at it.
type Component struct {
	ID      string   `yaml:"id" json:"id"`
	Name    string   `yaml:"name" json:"name"`
	About   string   `yaml:"about" json:"about"`
	Where   string   `yaml:"where" json:"where,omitempty"`
	Unit    string   `yaml:"unit" json:"unit,omitempty"`
	Listens []string `yaml:"listens" json:"listens,omitempty"`
	Config  []string `yaml:"config" json:"config,omitempty"`
	Files   []string `yaml:"files" json:"files,omitempty"`
	Logs    []string `yaml:"logs" json:"logs,omitempty"`
	Observe []string `yaml:"observe" json:"observe,omitempty"`
	Calls   []string `yaml:"calls" json:"calls,omitempty"`
}

// ParseArchitecture reads an architecture.yaml.
func ParseArchitecture(b []byte) (*Architecture, error) {
	var a Architecture
	if err := yaml.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (c Config) architecture(w http.ResponseWriter, r *http.Request) {
	a := &Architecture{}
	if c.Architecture != nil {
		var err error
		if a, err = ParseArchitecture(c.Architecture); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, a)
}

func (c Config) changes(w http.ResponseWriter, r *http.Request) {
	var v any = []any{}
	if c.Changes != nil {
		v = c.Changes()
	}
	writeJSON(w, v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
