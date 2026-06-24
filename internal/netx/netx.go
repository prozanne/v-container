// Package netx provides host-side networking helpers for QEMU user-mode
// (SLIRP) port forwarding. It is intentionally small and dependency-free so
// other packages can rely on it without import cycles.
//
// Security note: every host-side port forward emitted by this package is
// ALWAYS bound to the loopback address 127.0.0.1. Forwards are never exposed
// on a LAN-visible interface, so a guest service reachable via these clauses
// is only reachable from the host itself.
package netx

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// loopback is the host bind address every forward is pinned to.
const loopback = "127.0.0.1"

// netdevIDPattern is the allowlist for a -netdev id. It deliberately excludes
// commas, '=', whitespace and any other QEMU-structural or control characters.
// This prevents a caller-supplied id from injecting an extra QEMU option (for
// example an additional hostfwd clause bound to a non-loopback address), which
// would defeat this package's loopback-only security guarantee.
var netdevIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// freeLoopbackPortAttempts bounds how many times FreeLoopbackPort retries when
// the kernel keeps handing back a port the caller asked to avoid.
const freeLoopbackPortAttempts = 50

// FreeLoopbackPort returns an unused TCP port on 127.0.0.1 by binding to :0 and
// letting the kernel choose a free port. It optionally avoids the ports listed
// in 'avoid'. It returns an error if no acceptable port can be found.
//
// There is an inherent race: the returned port is free at the moment of the
// call but a concurrent process could claim it before the caller binds it
// again. Callers that need the port should bind it promptly.
func FreeLoopbackPort(avoid ...int) (int, error) {
	return freeLoopbackPort(bindEphemeral, avoid...)
}

// freeLoopbackPort is the testable core of FreeLoopbackPort. The bind function
// is injected so the retry/exhaustion branches can be exercised deterministically
// in tests; production code always passes bindEphemeral.
func freeLoopbackPort(bind func() (int, error), avoid ...int) (int, error) {
	if bind == nil {
		return 0, fmt.Errorf("netx: nil bind function")
	}

	avoidSet := make(map[int]struct{}, len(avoid))
	for _, p := range avoid {
		avoidSet[p] = struct{}{}
	}

	var lastErr error
	for attempt := 0; attempt < freeLoopbackPortAttempts; attempt++ {
		port, err := bind()
		if err != nil {
			lastErr = err
			continue
		}
		if _, skip := avoidSet[port]; skip {
			continue
		}
		return port, nil
	}

	if lastErr != nil {
		return 0, fmt.Errorf("netx: could not find a free loopback port after %d attempts: %w", freeLoopbackPortAttempts, lastErr)
	}
	return 0, fmt.Errorf("netx: could not find a free loopback port after %d attempts avoiding %v", freeLoopbackPortAttempts, avoid)
}

// bindEphemeral binds 127.0.0.1:0, reads the kernel-assigned port, closes the
// listener and returns the port. The port is known to have been bindable.
func bindEphemeral() (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(loopback, "0"))
	if err != nil {
		return 0, fmt.Errorf("netx: listen on %s:0: %w", loopback, err)
	}
	defer ln.Close()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("netx: unexpected listener address type %T", ln.Addr())
	}
	port := tcpAddr.Port
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("netx: kernel returned out-of-range port %d", port)
	}
	return port, nil
}

// Forward describes a single host-to-guest port forward. Proto is "tcp" or
// "udp"; an empty Proto defaults to "tcp".
type Forward struct {
	HostPort  int
	GuestPort int
	Proto     string
}

// normalizeProto validates and lowercases the protocol, defaulting empty to
// "tcp".
func normalizeProto(proto string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(proto)) {
	case "", "tcp":
		return "tcp", nil
	case "udp":
		return "udp", nil
	default:
		return "", fmt.Errorf("netx: invalid proto %q (want \"tcp\" or \"udp\")", proto)
	}
}

// validatePort ensures a port number is within the valid TCP/UDP range.
func validatePort(name string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("netx: %s %d out of range (want 1..65535)", name, port)
	}
	return nil
}

// validate checks the forward's ports and protocol, returning the normalized
// protocol on success.
func (f Forward) validate() (string, error) {
	proto, err := normalizeProto(f.Proto)
	if err != nil {
		return "", err
	}
	if err := validatePort("host port", f.HostPort); err != nil {
		return "", err
	}
	if err := validatePort("guest port", f.GuestPort); err != nil {
		return "", err
	}
	return proto, nil
}

// Hostfwd renders one QEMU hostfwd clause, ALWAYS bound to 127.0.0.1, e.g.
// "hostfwd=tcp:127.0.0.1:2222-:22". It returns an error on invalid ports or
// protocol.
func (f Forward) Hostfwd() (string, error) {
	proto, err := f.validate()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hostfwd=%s:%s:%d-:%d", proto, loopback, f.HostPort, f.GuestPort), nil
}

// BuildNetdev builds the full QEMU -netdev user value, e.g.
// "user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22,hostfwd=tcp:127.0.0.1:8080-:80".
// The id must be non-empty and match the allowlist [A-Za-z0-9_-]+ (no commas,
// '=', whitespace or control characters, which would otherwise let the id
// inject extra QEMU options and bypass the loopback-only guarantee). Every
// forward is validated. With no forwards it returns just "user,id=<id>".
func BuildNetdev(id string, fwds []Forward) (string, error) {
	if id == "" {
		return "", fmt.Errorf("netx: netdev id must be non-empty")
	}
	if !netdevIDPattern.MatchString(id) {
		return "", fmt.Errorf("netx: invalid netdev id %q (want non-empty, matching %s)", id, netdevIDPattern.String())
	}

	parts := make([]string, 0, len(fwds)+1)
	parts = append(parts, "user", "id="+id)
	for i, f := range fwds {
		clause, err := f.Hostfwd()
		if err != nil {
			return "", fmt.Errorf("netx: forward %d: %w", i, err)
		}
		parts = append(parts, clause)
	}
	return strings.Join(parts, ","), nil
}
