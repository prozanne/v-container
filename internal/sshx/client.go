package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// DialOptions configures a connection to a guest.
type DialOptions struct {
	Addr    string // "127.0.0.1:port"
	User    string
	Signer  ssh.Signer
	Timeout time.Duration // per-attempt dial+handshake timeout; 0 => 15s
}

// Client wraps an *ssh.Client with the address it connected to.
type Client struct {
	*ssh.Client
	Addr string
}

func (o DialOptions) validate() error {
	if o.Addr == "" {
		return errors.New("sshx: empty address")
	}
	if o.User == "" {
		return errors.New("sshx: empty user")
	}
	if o.Signer == nil {
		return errors.New("sshx: nil signer")
	}
	return nil
}

func (o DialOptions) timeout() time.Duration {
	if o.Timeout <= 0 {
		return 15 * time.Second
	}
	return o.Timeout
}

// Dial opens a single SSH connection. The host key is not verified because the
// endpoint is always a loopback port forwarded to our own freshly-built guest
// (whose host key changes on every rebuild); the connection never leaves the
// host. If that assumption ever changes, swap in a TOFU callback here.
func Dial(opts DialOptions) (*Client, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(opts.Signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // loopback to our own guest; see doc above
		Timeout:         opts.timeout(),
	}
	conn, err := net.DialTimeout("tcp", opts.Addr, opts.timeout())
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", opts.Addr, err)
	}
	// Bound the handshake too, so a half-open port can't hang us forever.
	_ = conn.SetDeadline(time.Now().Add(opts.timeout()))
	c, chans, reqs, err := ssh.NewClientConn(conn, opts.Addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh handshake with %s: %w", opts.Addr, err)
	}
	_ = conn.SetDeadline(time.Time{}) // clear deadline for the live session
	return &Client{Client: ssh.NewClient(c, chans, reqs), Addr: opts.Addr}, nil
}

// WaitForSSH retries Dial until it succeeds or ctx is cancelled. It is the
// canonical "wait for the guest to finish booting" call. On failure it returns
// the last dial error wrapped with context, so callers can surface a useful
// message (and pair it with the serial console log).
func WaitForSSH(ctx context.Context, opts DialOptions) (*Client, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastErr error
	for {
		client, err := Dial(opts)
		if err == nil {
			return client, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("timed out waiting for ssh on %s: %w", opts.Addr, lastErr)
			}
			return nil, fmt.Errorf("timed out waiting for ssh on %s: %w", opts.Addr, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Ping opens and immediately closes a connection, reporting reachability.
func Ping(opts DialOptions) bool {
	c, err := Dial(opts)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
