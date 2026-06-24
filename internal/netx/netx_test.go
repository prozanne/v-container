package netx

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestFreeLoopbackPort(t *testing.T) {
	port, err := FreeLoopbackPort()
	if err != nil {
		t.Fatalf("FreeLoopbackPort() error = %v", err)
	}
	if port < 1 || port > 65535 {
		t.Fatalf("FreeLoopbackPort() = %d, want in range 1..65535", port)
	}

	// The returned port must actually be bindable on loopback.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)))
	if err != nil {
		t.Fatalf("returned port %d is not bindable: %v", port, err)
	}
	ln.Close()
}

func TestFreeLoopbackPortAvoid(t *testing.T) {
	first, err := FreeLoopbackPort()
	if err != nil {
		t.Fatalf("first FreeLoopbackPort() error = %v", err)
	}

	second, err := FreeLoopbackPort(first)
	if err != nil {
		t.Fatalf("second FreeLoopbackPort(%d) error = %v", first, err)
	}
	if second == first {
		t.Fatalf("FreeLoopbackPort(%d) returned the avoided port %d", first, second)
	}
	if second < 1 || second > 65535 {
		t.Fatalf("FreeLoopbackPort(%d) = %d, want in range 1..65535", first, second)
	}

	// Avoiding multiple ports must also be honored.
	third, err := FreeLoopbackPort(first, second)
	if err != nil {
		t.Fatalf("FreeLoopbackPort(%d, %d) error = %v", first, second, err)
	}
	if third == first || third == second {
		t.Fatalf("FreeLoopbackPort(%d, %d) returned an avoided port %d", first, second, third)
	}
}

// TestFreeLoopbackPortAvoidRetry collects several real ephemeral ports, passes
// a subset as avoid, and asserts the result lands outside the avoid set. This
// exercises the avoid 'continue' (retry) path with a realistic avoid set.
func TestFreeLoopbackPortAvoidRetry(t *testing.T) {
	const sampleSize = 16
	avoid := make([]int, 0, sampleSize)
	avoidSet := make(map[int]struct{}, sampleSize)
	for i := 0; i < sampleSize; i++ {
		p, err := FreeLoopbackPort(avoid...)
		if err != nil {
			t.Fatalf("FreeLoopbackPort(%v) error = %v", avoid, err)
		}
		if _, dup := avoidSet[p]; dup {
			t.Fatalf("FreeLoopbackPort returned avoided port %d (avoid=%v)", p, avoid)
		}
		avoid = append(avoid, p)
		avoidSet[p] = struct{}{}
	}

	got, err := FreeLoopbackPort(avoid...)
	if err != nil {
		t.Fatalf("FreeLoopbackPort(%v) error = %v", avoid, err)
	}
	if _, bad := avoidSet[got]; bad {
		t.Fatalf("FreeLoopbackPort(%v) returned avoided port %d", avoid, got)
	}
}

// TestFreeLoopbackPortInjected drives the testable core with a fake binder to
// deterministically cover the retry and exhaustion branches that are otherwise
// impractical to trigger via the real kernel.
func TestFreeLoopbackPortInjected(t *testing.T) {
	t.Run("retries past avoided ports", func(t *testing.T) {
		seq := []int{4242, 4242, 4242, 5555}
		i := 0
		bind := func() (int, error) {
			p := seq[i]
			if i < len(seq)-1 {
				i++
			}
			return p, nil
		}
		got, err := freeLoopbackPort(bind, 4242)
		if err != nil {
			t.Fatalf("freeLoopbackPort error = %v", err)
		}
		if got != 5555 {
			t.Fatalf("freeLoopbackPort = %d, want 5555", got)
		}
	})

	t.Run("exhaustion with bind error", func(t *testing.T) {
		wantErr := fmt.Errorf("boom")
		bind := func() (int, error) { return 0, wantErr }
		_, err := freeLoopbackPort(bind)
		if err == nil {
			t.Fatal("freeLoopbackPort: want error, got nil")
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("freeLoopbackPort error = %v, want wrapped %v", err, wantErr)
		}
	})

	t.Run("exhaustion all avoided", func(t *testing.T) {
		bind := func() (int, error) { return 4242, nil }
		_, err := freeLoopbackPort(bind, 4242)
		if err == nil {
			t.Fatal("freeLoopbackPort: want error, got nil")
		}
		if !strings.Contains(err.Error(), "avoiding") {
			t.Fatalf("freeLoopbackPort error = %v, want message mentioning avoiding", err)
		}
	})

	t.Run("nil bind function", func(t *testing.T) {
		_, err := freeLoopbackPort(nil)
		if err == nil {
			t.Fatal("freeLoopbackPort(nil): want error, got nil")
		}
	})
}

// TestBindEphemeral verifies the production binder returns a usable, in-range
// loopback port.
func TestBindEphemeral(t *testing.T) {
	port, err := bindEphemeral()
	if err != nil {
		t.Fatalf("bindEphemeral() error = %v", err)
	}
	if port < 1 || port > 65535 {
		t.Fatalf("bindEphemeral() = %d, want in range 1..65535", port)
	}
}

func TestForwardHostfwd(t *testing.T) {
	tests := []struct {
		name    string
		fwd     Forward
		want    string
		wantErr bool
	}{
		{
			name: "tcp explicit",
			fwd:  Forward{HostPort: 2222, GuestPort: 22, Proto: "tcp"},
			want: "hostfwd=tcp:127.0.0.1:2222-:22",
		},
		{
			name: "default proto empty",
			fwd:  Forward{HostPort: 8080, GuestPort: 80},
			want: "hostfwd=tcp:127.0.0.1:8080-:80",
		},
		{
			name: "udp",
			fwd:  Forward{HostPort: 5353, GuestPort: 53, Proto: "udp"},
			want: "hostfwd=udp:127.0.0.1:5353-:53",
		},
		{
			name: "uppercase proto normalized",
			fwd:  Forward{HostPort: 2222, GuestPort: 22, Proto: "TCP"},
			want: "hostfwd=tcp:127.0.0.1:2222-:22",
		},
		{
			name: "proto with whitespace",
			fwd:  Forward{HostPort: 2222, GuestPort: 22, Proto: " udp "},
			want: "hostfwd=udp:127.0.0.1:2222-:22",
		},
		{
			name: "max ports",
			fwd:  Forward{HostPort: 65535, GuestPort: 65535, Proto: "tcp"},
			want: "hostfwd=tcp:127.0.0.1:65535-:65535",
		},
		{
			name:    "host port zero",
			fwd:     Forward{HostPort: 0, GuestPort: 22},
			wantErr: true,
		},
		{
			name:    "guest port zero",
			fwd:     Forward{HostPort: 2222, GuestPort: 0},
			wantErr: true,
		},
		{
			name:    "host port too large",
			fwd:     Forward{HostPort: 70000, GuestPort: 22},
			wantErr: true,
		},
		{
			name:    "guest port too large",
			fwd:     Forward{HostPort: 2222, GuestPort: 70000},
			wantErr: true,
		},
		{
			name:    "host port negative",
			fwd:     Forward{HostPort: -1, GuestPort: 22},
			wantErr: true,
		},
		{
			name:    "bad proto",
			fwd:     Forward{HostPort: 2222, GuestPort: 22, Proto: "sctp"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.fwd.Hostfwd()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Hostfwd() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Hostfwd() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Hostfwd() = %q, want %q", got, tt.want)
			}
			assertLoopbackOnly(t, got)
		})
	}
}

// assertLoopbackOnly verifies the rendered value binds to 127.0.0.1 and to no
// other (non-loopback) host address. A clause that contained 127.0.0.1 AND a
// non-loopback bind such as 0.0.0.0 must NOT slip through.
func assertLoopbackOnly(t *testing.T, got string) {
	t.Helper()
	if !strings.Contains(got, loopback) {
		t.Fatalf("value %q must bind to %s", got, loopback)
	}
	for _, bad := range []string{"0.0.0.0", "::", "[::]", "localhost"} {
		if strings.Contains(got, bad) {
			t.Fatalf("value %q must not contain non-loopback bind %q", got, bad)
		}
	}
}

func TestBuildNetdev(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		fwds    []Forward
		want    string
		wantErr bool
	}{
		{
			name: "two forwards",
			id:   "net0",
			fwds: []Forward{
				{HostPort: 2222, GuestPort: 22, Proto: "tcp"},
				{HostPort: 8080, GuestPort: 80},
			},
			want: "user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22,hostfwd=tcp:127.0.0.1:8080-:80",
		},
		{
			name: "no forwards",
			id:   "net0",
			fwds: nil,
			want: "user,id=net0",
		},
		{
			name: "single udp forward",
			id:   "n1",
			fwds: []Forward{{HostPort: 5353, GuestPort: 53, Proto: "udp"}},
			want: "user,id=n1,hostfwd=udp:127.0.0.1:5353-:53",
		},
		{
			name: "id with underscore and dash",
			id:   "net_0-a",
			fwds: nil,
			want: "user,id=net_0-a",
		},
		{
			name:    "empty id",
			id:      "",
			fwds:    nil,
			wantErr: true,
		},
		{
			name:    "whitespace id",
			id:      "   ",
			fwds:    nil,
			wantErr: true,
		},
		{
			name:    "id with surrounding whitespace",
			id:      " net0 ",
			fwds:    nil,
			wantErr: true,
		},
		{
			name:    "id with internal whitespace",
			id:      "net 0",
			fwds:    nil,
			wantErr: true,
		},
		{
			name:    "id with tab",
			id:      "net0\t",
			fwds:    nil,
			wantErr: true,
		},
		{
			// Injection attempt: a comma in the id would smuggle an extra
			// QEMU option (here a non-loopback hostfwd) into the -netdev value.
			name:    "id injecting comma and extra hostfwd",
			id:      "net0,hostfwd=tcp:0.0.0.0:1-:1",
			fwds:    nil,
			wantErr: true,
		},
		{
			name:    "id with equals",
			id:      "net0=x",
			fwds:    nil,
			wantErr: true,
		},
		{
			name:    "id with colon",
			id:      "net0:1",
			fwds:    nil,
			wantErr: true,
		},
		{
			name:    "id with control char",
			id:      "net0\n",
			fwds:    nil,
			wantErr: true,
		},
		{
			name:    "invalid forward propagates error",
			id:      "net0",
			fwds:    []Forward{{HostPort: 0, GuestPort: 22}},
			wantErr: true,
		},
		{
			name:    "invalid proto propagates error",
			id:      "net0",
			fwds:    []Forward{{HostPort: 2222, GuestPort: 22, Proto: "icmp"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildNetdev(tt.id, tt.fwds)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("BuildNetdev() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildNetdev() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("BuildNetdev() = %q, want %q", got, tt.want)
			}
			// The whole value (id + every clause) must be loopback-only: it
			// must contain no non-loopback bind address anywhere.
			if strings.Contains(got, "hostfwd=") {
				assertLoopbackOnly(t, got)
			}
			// Every forwarded clause must individually be loopback-bound.
			for _, part := range strings.Split(got, ",") {
				if strings.HasPrefix(part, "hostfwd=") && !strings.Contains(part, loopback) {
					t.Fatalf("BuildNetdev() clause %q not loopback-bound", part)
				}
			}
		})
	}
}
