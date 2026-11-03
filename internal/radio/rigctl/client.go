package rigctl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultTimeout = 2 * time.Second

type Client struct {
	Address string
	Timeout time.Duration
}

func New(address string) *Client {
	return &Client{Address: strings.TrimSpace(address), Timeout: defaultTimeout}
}

type Session struct {
	mu      sync.Mutex
	conn    net.Conn
	reader  *bufio.Reader
	timeout time.Duration
	closed  bool
}

func (c *Client) StartPTT(ctx context.Context) (*Session, error) {
	if c == nil || strings.TrimSpace(c.Address) == "" {
		return nil, errors.New("rigctld address is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return nil, fmt.Errorf("connect to rigctld %s: %w", c.Address, err)
	}
	s := &Session{conn: conn, reader: bufio.NewReader(conn), timeout: timeout}
	if err := s.SetPTT(ctx, true); err != nil {
		// Best effort: if rigctld accepted T 1 but the response was lost, try
		// T 0 on the same session before dropping the TCP connection.
		releaseCtx, cancel := context.WithTimeout(context.Background(), timeout)
		_ = s.SetPTT(releaseCtx, false)
		cancel()
		_ = s.Close()
		return nil, fmt.Errorf("assert Hamlib PTT: %w", err)
	}
	return s, nil
}

func (s *Session) SetPTT(ctx context.Context, enabled bool) error {
	if s == nil {
		return errors.New("nil rigctld session")
	}
	value := 0
	if enabled {
		value = 1
	}
	_, err := s.command(ctx, fmt.Sprintf("T %d\n", value))
	return err
}

func (s *Session) command(ctx context.Context, command string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.conn == nil {
		return "", errors.New("rigctld session is closed")
	}
	deadline := time.Now().Add(s.timeout)
	if ctx != nil {
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
	}
	if err := s.conn.SetDeadline(deadline); err != nil {
		return "", err
	}
	if _, err := s.conn.Write([]byte(command)); err != nil {
		_ = s.closeLocked()
		return "", fmt.Errorf("write rigctld command: %w", err)
	}
	line, err := s.reader.ReadString('\n')
	if err != nil {
		_ = s.closeLocked()
		return "", fmt.Errorf("read rigctld response: %w", err)
	}
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "RPRT ") {
		code, parseErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "RPRT ")))
		if parseErr != nil {
			return line, fmt.Errorf("invalid rigctld response %q", line)
		}
		if code != 0 {
			return line, fmt.Errorf("rigctld returned error code %d", code)
		}
	}
	return line, nil
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}
func (s *Session) closeLocked() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	return err
}
