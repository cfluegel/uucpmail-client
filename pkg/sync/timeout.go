package sync

import (
	"fmt"
	"io"
	"time"
)

// sessionReadTimeout entspricht Taylors cTtimeout (120 s): bleibt der
// Server so lange stumm (z.B. weil er ein Command verworfen hat und
// seinerseits wartet), endet die Session mit Fehler statt zu hängen.
const sessionReadTimeout = 120 * time.Second

// readTimeoutConn überwacht jedes Read mit einem Timer. SSH-Channels
// kennen kein SetReadDeadline, daher läuft das Read in einer Goroutine;
// bei Timeout wird die Verbindung geschlossen, was das hängende Read
// entblockt.
type readTimeoutConn struct {
	rw      io.ReadWriter
	closer  io.Closer
	timeout time.Duration
}

func (c *readTimeoutConn) Write(p []byte) (int, error) { return c.rw.Write(p) }

func (c *readTimeoutConn) Read(p []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := c.rw.Read(p)
		ch <- result{n, err}
	}()
	timer := time.NewTimer(c.timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.n, r.err
	case <-timer.C:
		if c.closer != nil {
			_ = c.closer.Close()
		}
		return 0, fmt.Errorf("keine Antwort vom Server innerhalb von %s – Verbindung abgebrochen", c.timeout)
	}
}
