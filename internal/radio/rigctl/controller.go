package rigctl

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type PTTController struct {
	mu      sync.Mutex
	client  *Client
	session *Session
	closed  bool
}

func NewPTTController(client *Client) (*PTTController, error) {
	if client == nil {
		return nil, errors.New("nil rigctld client")
	}
	return &PTTController{client: client}, nil
}
func (c *PTTController) SetPTT(ctx context.Context, enabled bool) error {
	if c == nil {
		return errors.New("nil Hamlib PTT controller")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("Hamlib PTT controller is closed")
	}
	if enabled {
		if c.session != nil {
			return nil
		}
		s, err := c.client.StartPTT(ctx)
		if err != nil {
			return err
		}
		c.session = s
		return nil
	}
	if c.session == nil {
		return nil
	}
	s := c.session
	if err := s.SetPTT(ctx, false); err != nil {
		return fmt.Errorf("send rigctld PTT-off: %w", err)
	}
	if err := s.Close(); err != nil {
		return fmt.Errorf("close rigctld session after PTT-off: %w", err)
	}
	c.session = nil
	return nil
}
func (c *PTTController) Close() error {
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.session == nil {
		return nil
	}
	releaseErr := c.session.SetPTT(ctx, false)
	closeErr := c.session.Close()
	c.session = nil
	if releaseErr != nil {
		return fmt.Errorf("release Hamlib PTT during close: %w", releaseErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close rigctld session: %w", closeErr)
	}
	return nil
}
