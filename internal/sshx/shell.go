package sshx

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Shell opens an interactive login shell on the guest, wiring it to the local
// terminal. It puts the local terminal into raw mode (restoring it on exit),
// requests a remote PTY, and forwards terminal resize events by polling the
// local size — which works identically on Windows and Unix (no SIGWINCH needed).
//
// If stdin is not a terminal (e.g. piped), it falls back to a non-PTY session
// so scripted use still works.
func (c *Client) Shell(ctx context.Context) error {
	sess, err := c.NewSession()
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	defer sess.Close()

	sess.Stdin = os.Stdin
	sess.Stdout = os.Stdout
	sess.Stderr = os.Stderr

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Non-interactive stdin: just run the default shell without a PTY.
		if err := sess.Shell(); err != nil {
			return fmt.Errorf("start shell: %w", err)
		}
		return waitShell(ctx, sess)
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("set raw terminal: %w", err)
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	w, h := 80, 24
	if cw, ch, err := term.GetSize(fd); err == nil {
		w, h = cw, ch
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	termType := os.Getenv("TERM")
	if termType == "" {
		termType = "xterm-256color"
	}
	if err := sess.RequestPty(termType, h, w, modes); err != nil {
		return fmt.Errorf("request pty: %w", err)
	}

	if err := sess.Shell(); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	// Forward terminal resizes by polling local size every 250ms.
	stopResize := make(chan struct{})
	go watchResize(fd, sess, w, h, stopResize)
	defer close(stopResize)

	return waitShell(ctx, sess)
}

func waitShell(ctx context.Context, sess *ssh.Session) error {
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		_ = sess.Close()
		<-done
		return ctx.Err()
	case err := <-done:
		// A remote shell exiting non-zero (e.g. Ctrl-D after a failed command)
		// is normal; don't treat ExitError/ExitMissingError as a tool failure.
		if _, cerr := exitCode(err); cerr != nil {
			return cerr
		}
		return nil
	}
}

func watchResize(fd int, sess *ssh.Session, w, h int, stop <-chan struct{}) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	lastW, lastH := w, h
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			nw, nh, err := term.GetSize(fd)
			if err != nil || (nw == lastW && nh == lastH) {
				continue
			}
			lastW, lastH = nw, nh
			_ = sess.WindowChange(nh, nw)
		}
	}
}
