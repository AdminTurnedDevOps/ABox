package runtime

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func TestUserTurnCtxRejectsV1ForRich(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	s := &Sandbox{conn: a, GuestProtocol: 1}
	_, err := s.UserTurnCtx(context.Background(), "hi", TurnOptions{RichEvents: true}, nil)
	if !errors.Is(err, ErrGuestTooOld) {
		t.Fatalf("got %v", err)
	}
	b.Close()
}

func TestUserTurnPlainOnPipe(t *testing.T) {
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })
	s := &Sandbox{conn: host, GuestProtocol: 2}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		frame, err := protocol.ReadFrame(guest)
		if err != nil {
			t.Errorf("read turn: %v", err)
			return
		}
		if frame.Method != "user_turn" {
			t.Errorf("method %q", frame.Method)
			return
		}
		ev, _ := protocol.EncodeParams(protocol.AgentEvent{Kind: "text", Text: "ok"})
		_ = protocol.WriteFrame(guest, protocol.Frame{ID: frame.ID, Method: "agent_event", Params: ev})
		ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
		_ = protocol.WriteFrame(guest, protocol.Frame{ID: frame.ID, Result: ok})
	}()

	var got []string
	err := s.UserTurn(context.Background(), "hi", func(e protocol.AgentEvent) {
		got = append(got, e.Kind+":"+e.Text)
	})
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "text:ok" {
		t.Fatalf("%v", got)
	}
}

func TestUserTurnCtxCancelWritesCancelTurn(t *testing.T) {
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })
	s := &Sandbox{conn: host, GuestProtocol: 2}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	go func() {
		frame, err := protocol.ReadFrame(guest)
		if err != nil {
			done <- "read-turn:" + err.Error()
			return
		}
		cancel()
		_ = guest.SetDeadline(time.Now().Add(2 * time.Second))
		cf, err := protocol.ReadFrame(guest)
		if err != nil {
			done <- "read-cancel:" + err.Error()
			return
		}
		if cf.Method != "cancel_turn" {
			done <- "method:" + cf.Method
			return
		}
		_ = protocol.WriteFrame(guest, protocol.Frame{ID: frame.ID, Error: &protocol.Error{Code: "canceled", Message: "canceled"}})
		done <- "ok"
	}()

	out, err := s.UserTurnCtx(ctx, "hi", TurnOptions{}, nil)
	got := <-done
	if got != "ok" {
		t.Fatalf("guest side: %s (err=%v out=%+v)", got, err, out)
	}
	if out == nil || !out.Canceled {
		t.Fatalf("expected canceled, err=%v out=%+v", err, out)
	}
}

func TestWaitHelloStoresProtocol(t *testing.T) {
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })
	s := &Sandbox{conn: host, Sess: &session.Session{ID: "sid", Capability: "cap"}}
	go func() {
		params, _ := protocol.EncodeParams(protocol.HelloParams{
			SessionID: "sid", Capability: "cap", Protocol: 2, GuestReady: true,
		})
		_ = protocol.WriteFrame(guest, protocol.Frame{ID: "hello", Method: "hello", Params: params})
		_, _ = protocol.ReadFrame(guest)
		guest.Close()
	}()
	if err := s.waitHello(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.GuestProtocol != 2 {
		t.Fatalf("protocol %d", s.GuestProtocol)
	}
}

func TestWaitHelloDefaultsV1(t *testing.T) {
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })
	s := &Sandbox{conn: host, Sess: &session.Session{ID: "sid", Capability: "cap"}}
	go func() {
		params, _ := protocol.EncodeParams(protocol.HelloParams{
			SessionID: "sid", Capability: "cap", GuestReady: true,
		})
		_ = protocol.WriteFrame(guest, protocol.Frame{ID: "hello", Method: "hello", Params: params})
		_, _ = protocol.ReadFrame(guest)
		guest.Close()
	}()
	if err := s.waitHello(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.GuestProtocol != 1 {
		t.Fatalf("protocol %d", s.GuestProtocol)
	}
}
