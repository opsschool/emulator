package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// progress shows a bar and the time taken while a slow step runs, so a
// quiet boot doesn't look hung. How long a boot takes varies, so the bar
// fills towards expect and waits just short of the end if it runs over.
// Off a terminal it only prints the messages.
type progress struct {
	w      io.Writer
	tty    bool
	expect time.Duration
	start  time.Time
	mu     sync.Mutex
	stop   chan struct{}
	done   chan struct{}
}

func startProgress(w io.Writer, expect time.Duration) *progress {
	p := &progress{w: w, tty: isTerminal(w), expect: expect, start: time.Now(),
		stop: make(chan struct{}), done: make(chan struct{})}
	if !p.tty {
		close(p.done)
		return p
	}
	go func() {
		defer close(p.done)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			p.mu.Lock()
			p.draw()
			p.mu.Unlock()
			select {
			case <-p.stop:
				return
			case <-t.C:
			}
		}
	}()
	return p
}

// Say prints a message above the bar.
func (p *progress) Say(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tty {
		fmt.Fprint(p.w, "\r\033[K")
	}
	fmt.Fprintln(p.w, "==> "+msg)
	if p.tty {
		p.draw()
	}
}

// Done removes the bar.
func (p *progress) Done() {
	if p.tty {
		close(p.stop)
	}
	<-p.done
	if p.tty {
		fmt.Fprint(p.w, "\r\033[K")
	}
}

func (p *progress) draw() {
	el := time.Since(p.start)
	fmt.Fprintf(p.w, "\r    %s %s ", bar(el, p.expect, 30), clock(el))
}

// bar draws how far elapsed is through expect, never quite full.
func bar(elapsed, expect time.Duration, width int) string {
	n := int(float64(width) * float64(elapsed) / float64(expect))
	n = min(max(n, 0), width-1)
	return "[" + strings.Repeat("█", n) + strings.Repeat("░", width-n) + "]"
}
