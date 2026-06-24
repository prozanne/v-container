package qemu

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// qmpResponse is the subset of a QMP reply we inspect.
type qmpResponse struct {
	Return *json.RawMessage `json:"return"`
	Error  *struct {
		Class string `json:"class"`
		Desc  string `json:"desc"`
	} `json:"error"`
	Event string `json:"event"`
}

// qmpExec connects to a QMP TCP endpoint, completes the capabilities handshake,
// and executes a single command, returning any QMP-reported error.
func qmpExec(addr, command string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("connect QMP %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	// 1. Greeting.
	var greeting map[string]json.RawMessage
	if err := dec.Decode(&greeting); err != nil {
		return fmt.Errorf("read QMP greeting: %w", err)
	}
	if _, ok := greeting["QMP"]; !ok {
		return fmt.Errorf("unexpected QMP greeting from %s", addr)
	}

	// 2. Enter command mode.
	if err := enc.Encode(map[string]any{"execute": "qmp_capabilities"}); err != nil {
		return fmt.Errorf("send qmp_capabilities: %w", err)
	}
	if err := readResult(dec); err != nil {
		return fmt.Errorf("qmp_capabilities: %w", err)
	}

	// 3. The actual command.
	if err := enc.Encode(map[string]any{"execute": command}); err != nil {
		return fmt.Errorf("send %s: %w", command, err)
	}
	if err := readResult(dec); err != nil {
		return fmt.Errorf("%s: %w", command, err)
	}
	return nil
}

// readResult reads QMP messages, skipping asynchronous events, until it sees a
// return or error reply.
func readResult(dec *json.Decoder) error {
	for {
		var resp qmpResponse
		if err := dec.Decode(&resp); err != nil {
			return err
		}
		if resp.Event != "" {
			continue // async event, keep reading
		}
		if resp.Error != nil {
			return fmt.Errorf("QMP error: %s", resp.Error.Desc)
		}
		if resp.Return != nil {
			return nil
		}
		// Neither event/error/return (shouldn't happen): keep reading.
	}
}

// Powerdown sends an ACPI power-button event so the guest shuts down cleanly.
func Powerdown(qmpAddr string, timeout time.Duration) error {
	return qmpExec(qmpAddr, "system_powerdown", timeout)
}

// Quit asks QEMU to exit immediately (used as a fallback when the guest does
// not respond to the ACPI powerdown).
func Quit(qmpAddr string, timeout time.Duration) error {
	return qmpExec(qmpAddr, "quit", timeout)
}
