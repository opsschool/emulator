// Package logging sets up the shop's structured logs.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// New returns a JSON logger that writes to stdout (journald, under systemd)
// and, when file is set, appends to that file too. A failed file write never
// stops the program.
func New(level, file string) (*slog.Logger, error) {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(level)); err != nil {
		return nil, err
	}
	var w io.Writer = os.Stdout
	if file != "" {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		w = io.MultiWriter(os.Stdout, bestEffort{f})
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lv})), nil
}

// bestEffort ignores write errors so a full disk doesn't silence stdout.
type bestEffort struct{ w io.Writer }

func (b bestEffort) Write(p []byte) (int, error) {
	b.w.Write(p)
	return len(p), nil
}
