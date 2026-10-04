package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// resizeMsg is the one control message the page sends, as a text frame.
// Keystrokes and terminal output travel as binary frames.
type resizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// terminal runs a shell on the scenario machine in a pseudo-terminal and
// connects it to the page over a WebSocket. Each connection gets its own
// shell, which ends when the page disconnects.
func (c Config) terminal(w http.ResponseWriter, r *http.Request) {
	argv := c.Shell()
	if len(argv) == 0 {
		http.Error(w, "no shell for this machine", http.StatusServiceUnavailable)
		return
	}
	// Accept checks that the Origin matches the Host.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20) // a large paste

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		conn.Close(websocket.StatusInternalError, "could not start the shell")
		c.logf("terminal: %v", err)
		return
	}
	defer func() {
		f.Close()
		cmd.Process.Signal(syscall.SIGHUP)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}()

	// Shell output to the page. When the shell exits, reading fails and
	// the page is told the shell ended.
	go func() {
		defer cancel()
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				conn.Close(websocket.StatusNormalClosure, "the shell ended")
				return
			}
		}
	}()

	// Keystrokes and resizes from the page.
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
				c.logf("terminal: %v", err)
			}
			return
		}
		if typ == websocket.MessageText {
			var m resizeMsg
			if json.Unmarshal(data, &m) == nil && m.Type == "resize" && m.Cols > 0 && m.Rows > 0 {
				pty.Setsize(f, &pty.Winsize{Cols: m.Cols, Rows: m.Rows})
			}
			continue
		}
		if _, err := f.Write(data); err != nil {
			return
		}
	}
}

func (c Config) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log.Printf(format, args...)
	}
}
