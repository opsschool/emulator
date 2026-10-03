// Package logging sets up the shop's structured logs.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// New returns a JSON logger that writes to stdout (journald, under systemd)
// and, when file is set, appends to that file too. A failed file write never
// stops the program.
//
// Stdout lines start with a syslog priority such as <3>, which journald
// strips and records as the entry's priority, so `journalctl -p err` works.
func New(level, file string) (*slog.Logger, error) { return newLogger(level, file, os.Stdout) }

func newLogger(level, file string, stdout io.Writer) (*slog.Logger, error) {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(level)); err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: lv}
	hs := fanout{newJournalHandler(stdout, opts)}
	if file != "" {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		hs = append(hs, slog.NewJSONHandler(bestEffort{f}, opts))
	}
	return slog.New(hs), nil
}

// bestEffort ignores write errors so a full disk doesn't silence stdout.
type bestEffort struct{ w io.Writer }

func (b bestEffort) Write(p []byte) (int, error) {
	b.w.Write(p)
	return len(p), nil
}

// fanout sends each record to every handler.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range f {
		if h.Enabled(ctx, r.Level) {
			h.Handle(ctx, r.Clone())
		}
	}
	return nil
}

func (f fanout) WithAttrs(as []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(as)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}

// journalHandler writes JSON lines prefixed with the record's syslog
// priority: one JSON handler per priority, each behind a prefixing writer.
type journalHandler struct {
	debug, info, warn, err slog.Handler
}

func newJournalHandler(w io.Writer, opts *slog.HandlerOptions) journalHandler {
	h := func(p string) slog.Handler { return slog.NewJSONHandler(prefixWriter{w, []byte(p)}, opts) }
	return journalHandler{debug: h("<7>"), info: h("<6>"), warn: h("<4>"), err: h("<3>")}
}

func (j journalHandler) pick(l slog.Level) slog.Handler {
	switch {
	case l >= slog.LevelError:
		return j.err
	case l >= slog.LevelWarn:
		return j.warn
	case l >= slog.LevelInfo:
		return j.info
	}
	return j.debug
}

func (j journalHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return j.pick(l).Enabled(ctx, l)
}

func (j journalHandler) Handle(ctx context.Context, r slog.Record) error {
	return j.pick(r.Level).Handle(ctx, r)
}

func (j journalHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return journalHandler{j.debug.WithAttrs(as), j.info.WithAttrs(as), j.warn.WithAttrs(as), j.err.WithAttrs(as)}
}

func (j journalHandler) WithGroup(name string) slog.Handler {
	return journalHandler{j.debug.WithGroup(name), j.info.WithGroup(name), j.warn.WithGroup(name), j.err.WithGroup(name)}
}

// prefixWriter writes the prefix and the line in one call, so lines from
// concurrent goroutines don't interleave. slog handlers write a line per call.
type prefixWriter struct {
	w      io.Writer
	prefix []byte
}

func (p prefixWriter) Write(b []byte) (int, error) {
	buf := make([]byte, 0, len(p.prefix)+len(b))
	buf = append(append(buf, p.prefix...), b...)
	if _, err := p.w.Write(buf); err != nil {
		return 0, err
	}
	return len(b), nil
}
