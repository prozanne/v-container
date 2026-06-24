package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// exitCode extracts a process exit status from an ssh run error. A clean run
// returns (0, nil); a non-zero exit returns (code, nil); anything else (e.g. a
// transport failure) returns (-1, err).
func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		return ee.ExitStatus(), nil
	}
	var me *ssh.ExitMissingError
	if errors.As(err, &me) {
		return -1, fmt.Errorf("remote process exited without status: %w", err)
	}
	return -1, err
}

// Run executes cmd, capturing stdout and stderr. It returns the captured
// streams and the process exit code. A non-zero exit is NOT a Go error (err is
// nil, exit is the code); only transport/protocol failures populate err.
func (c *Client) Run(ctx context.Context, cmd string) (stdout, stderr string, exit int, err error) {
	sess, err := c.NewSession()
	if err != nil {
		return "", "", -1, fmt.Errorf("open session: %w", err)
	}
	defer sess.Close()

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	if err := runWithContext(ctx, sess, cmd); err != nil {
		code, cerr := exitCode(err)
		return outBuf.String(), errBuf.String(), code, cerr
	}
	return outBuf.String(), errBuf.String(), 0, nil
}

// Stream executes cmd wiring the provided stdin/stdout/stderr directly, for
// commands whose output should flow to the user in real time. It returns the
// exit code; only transport failures populate err.
func (c *Client) Stream(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (exit int, err error) {
	sess, err := c.NewSession()
	if err != nil {
		return -1, fmt.Errorf("open session: %w", err)
	}
	defer sess.Close()

	sess.Stdin = stdin
	sess.Stdout = stdout
	sess.Stderr = stderr

	if err := runWithContext(ctx, sess, cmd); err != nil {
		return exitCode(err)
	}
	return 0, nil
}

// runWithContext starts cmd and waits for it, aborting the session if ctx is
// cancelled so a hung remote command cannot block the caller indefinitely.
func runWithContext(ctx context.Context, sess *ssh.Session, cmd string) error {
	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("start %q: %w", cmd, err)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		// Best-effort signal then close; ignore errors (the server may not
		// honor SIGTERM over SSH, so Close is the real lever).
		_ = sess.Signal(ssh.SIGTERM)
		_ = sess.Close()
		<-done // let the goroutine finish
		return fmt.Errorf("command cancelled: %w", ctx.Err())
	case err := <-done:
		return err
	}
}
