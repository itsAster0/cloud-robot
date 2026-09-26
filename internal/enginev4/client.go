// Package enginev4 owns the private Rust-worker transport. Browser and robot
// connections stay in the Go API; no engine call waits for a remote controller.
package enginev4

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const MaxFrame = 16 * 1024 * 1024
const Version = 4

type Result struct {
	Geometry     json.RawMessage            `json:"geometry"`
	Snapshot     json.RawMessage            `json:"snapshot"`
	Observations map[string]json.RawMessage `json:"observations"`
	StateHash    string                     `json:"stateHash"`
}
type Client struct {
	conn net.Conn
	cmd  *exec.Cmd
	dir  string
	done chan error
}

func Start(ctx context.Context, executable string, config any) (*Client, Result, error) {
	return startWithKind(ctx, executable, "start", config)
}
func Restore(ctx context.Context, executable string, checkpoint any) (*Client, Result, error) {
	return startWithKind(ctx, executable, "restore", checkpoint)
}
func startWithKind(ctx context.Context, executable, kind string, config any) (*Client, Result, error) {
	dir, err := os.MkdirTemp("/tmp", "ra-")
	if err != nil {
		return nil, Result{}, err
	}
	path := filepath.Join(dir, "w.sock")
	c := &Client{dir: dir, done: make(chan error, 1)}
	c.cmd = exec.CommandContext(ctx, executable, path)
	c.cmd.Stderr = os.Stderr
	if err = c.cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, Result{}, err
	}
	go func() { c.done <- c.cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.conn, err = net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			c.Close()
			return nil, Result{}, ctx.Err()
		case err := <-c.done:
			os.RemoveAll(dir)
			return nil, Result{}, fmt.Errorf("worker exited: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if c.conn == nil {
		c.Close()
		return nil, Result{}, errors.New("worker socket did not become ready")
	}
	var result Result
	err = c.Call(ctx, kind, config, &result)
	if err != nil {
		c.Close()
		return nil, Result{}, err
	}
	return c, result, nil
}
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = os.RemoveAll(c.dir)
}
func (c *Client) Call(ctx context.Context, kind string, input any, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = c.conn.SetDeadline(deadline); err != nil {
		return err
	}
	if err = WriteEnvelope(c.conn, kind, payload); err != nil {
		return err
	}
	response, body, err := ReadEnvelope(c.conn)
	if err != nil {
		return err
	}
	if response == "error" {
		var e struct {
			Error string `json:"error"`
		}
		if err = json.Unmarshal(body, &e); err != nil {
			return err
		}
		return &Rejection{Message: e.Error}
	}
	return json.Unmarshal(body, output)
}

// These codecs implement the three-field Envelope in protocol/worker.proto.
// Unknown fields are skipped for forward-compatible framing; payload schemas
// are independently versioned and validated by the worker.
func WriteEnvelope(w io.Writer, kind string, payload []byte) error {
	b := []byte{8, Version, 18}
	b = binary.AppendUvarint(b, uint64(len(kind)))
	b = append(b, kind...)
	b = append(b, 26)
	b = binary.AppendUvarint(b, uint64(len(payload)))
	b = append(b, payload...)
	if len(b) > MaxFrame {
		return errors.New("frame too large")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(b)))
	if err := writeAll(w, size[:]); err != nil {
		return err
	}
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func ReadEnvelope(r io.Reader) (string, []byte, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return "", nil, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n > MaxFrame {
		return "", nil, errors.New("frame too large")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", nil, err
	}
	kind := ""
	var payload []byte
	var version uint64
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return "", nil, errors.New("invalid protobuf tag")
		}
		b = b[n:]
		switch tag & 7 {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return "", nil, errors.New("invalid varint")
			}
			if tag>>3 == 1 {
				version = v
			}
			b = b[n:]
		case 2:
			length, n := binary.Uvarint(b)
			if n <= 0 || length > uint64(len(b)-n) {
				return "", nil, errors.New("invalid protobuf length")
			}
			b = b[n:]
			v := b[:int(length)]
			b = b[int(length):]
			switch tag >> 3 {
			case 2:
				kind = string(v)
			case 3:
				payload = v
			}
		case 1:
			if len(b) < 8 {
				return "", nil, io.ErrUnexpectedEOF
			}
			b = b[8:]
		case 5:
			if len(b) < 4 {
				return "", nil, io.ErrUnexpectedEOF
			}
			b = b[4:]
		default:
			return "", nil, errors.New("unsupported protobuf wire type")
		}
	}
	if version != Version || kind == "" || payload == nil {
		return "", nil, errors.New("invalid worker envelope")
	}
	return kind, payload, nil
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// Rejection is a validated rule error; the worker remains usable.
type Rejection struct{ Message string }

func (e *Rejection) Error() string { return e.Message }
