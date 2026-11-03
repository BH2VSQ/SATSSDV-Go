package rigctl

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPTTControllerUsesSameSessionForOnAndOff(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	commands := make(chan string, 2)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		for i := 0; i < 2; i++ {
			line, e := r.ReadString('\n')
			if e != nil {
				return
			}
			commands <- strings.TrimSpace(line)
			_, _ = fmt.Fprintln(conn, "RPRT 0")
		}
	}()
	c, _ := NewPTTController(New(ln.Addr().String()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.SetPTT(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPTT(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got := <-commands; got != "T 1" {
		t.Fatalf("command=%q", got)
	}
	if got := <-commands; got != "T 0" {
		t.Fatalf("command=%q", got)
	}
}

func TestRigctlRejectsErrorResponse(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, e := ln.Accept()
		if e == nil {
			defer c.Close()
			_, _ = bufio.NewReader(c).ReadString('\n')
			_, _ = fmt.Fprintln(c, "RPRT -1")
		}
	}()
	_, err := New(ln.Addr().String()).StartPTT(context.Background())
	if err == nil {
		t.Fatal("expected rigctld error")
	}
}

func TestFailedPTTAssertionAttemptsOffOnSameSession(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	commands := make(chan string, 2)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		for i, response := range []string{"RPRT -1", "RPRT 0"} {
			line, e := r.ReadString('\n')
			if e != nil {
				return
			}
			commands <- strings.TrimSpace(line)
			_, _ = fmt.Fprintln(conn, response)
			if i == 1 {
				return
			}
		}
	}()
	if _, err := New(ln.Addr().String()).StartPTT(context.Background()); err == nil {
		t.Fatal("expected PTT assertion error")
	}
	if got := <-commands; got != "T 1" {
		t.Fatalf("first command=%q", got)
	}
	if got := <-commands; got != "T 0" {
		t.Fatalf("release command=%q", got)
	}
}
