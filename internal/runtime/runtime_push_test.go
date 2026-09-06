package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

type fakeGuest struct {
	t          *testing.T
	conn       net.Conn
	writeMu    sync.Mutex
	onRequest  func(frame protocol.Frame, reply func(protocol.Frame))
	onResponse func(frame protocol.Frame)
}

func (g *fakeGuest) write(f protocol.Frame) {
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	if err := protocol.WriteFrame(g.conn, f); err != nil {
		g.t.Errorf("guest write: %v", err)
	}
}

func (g *fakeGuest) serve() {
	for {
		frame, err := protocol.ReadFrame(g.conn)
		if err != nil {
			return
		}
		if frame.Method == "" {
			if g.onResponse != nil {
				g.onResponse(frame)
			}
			continue
		}
		if g.onRequest != nil {
			g.onRequest(frame, g.write)
		}
	}
}

func newPipeSandbox(t *testing.T, guestProtocol int) (*Sandbox, *fakeGuest) {
	t.Helper()
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })
	s := &Sandbox{conn: host, GuestProtocol: guestProtocol, calls: map[string]*frameQueue{}}
	g := &fakeGuest{t: t, conn: guest}
	go g.serve()
	return s, g
}

func TestPushSecretsOrderAndSplit(t *testing.T) {
	s, g := newPipeSandbox(t, 2)
	var order []string
	var mu sync.Mutex
	g.onRequest = func(frame protocol.Frame, reply func(protocol.Frame)) {
		mu.Lock()
		order = append(order, frame.Method)
		mu.Unlock()
		switch frame.Method {
		case "set_model":
			p, err := protocol.DecodeParams[protocol.SetModelParams](frame.Params)
			if err != nil {
				t.Errorf("set_model params: %v", err)
			}
			if p.Secrets["XAI_API_KEY"] != "mk" {
				t.Errorf("set_model missing model credential: %v", p.Secrets)
			}
			if len(p.Secrets) != 1 {
				t.Errorf("set_model carries more than the model credential: %v", p.Secrets)
			}
		case "set_mcp_tokens":
			p, err := protocol.DecodeParams[protocol.SetMCPTokensParams](frame.Params)
			if err != nil {
				t.Errorf("set_mcp_tokens params: %v", err)
			}
			if p.Secrets["ABOX_MCP_GH_TOKEN"] != "mt" {
				t.Errorf("set_mcp_tokens missing mcp token: %v", p.Secrets)
			}
			if _, ok := p.Secrets["XAI_API_KEY"]; ok {
				t.Errorf("model credential leaked into set_mcp_tokens: %v", p.Secrets)
			}
		default:
			t.Errorf("unexpected method %q", frame.Method)
		}
		ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
		reply(protocol.Frame{ID: frame.ID, Result: ok})
	}

	model := config.Model{Name: "grok-default", Provider: "xai", CredentialEnv: "XAI_API_KEY"}
	err := s.PushSecrets(context.Background(), model, map[string]string{
		"XAI_API_KEY": "mk", "ABOX_MCP_GH_TOKEN": "mt",
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "set_model" || order[1] != "set_mcp_tokens" {
		t.Fatalf("order %v", order)
	}
}

func TestPushSecretsProto3FiltersModelCredential(t *testing.T) {
	s, g := newPipeSandbox(t, 3)
	g.onRequest = func(frame protocol.Frame, reply func(protocol.Frame)) {
		switch frame.Method {
		case "set_model":
			p, err := protocol.DecodeParams[protocol.SetModelParams](frame.Params)
			if err != nil {
				t.Errorf("params: %v", err)
			}
			if len(p.Secrets) != 0 {
				t.Errorf("proto-3 set_model must carry no secrets: %v", p.Secrets)
			}
		case "set_mcp_tokens":
			p, _ := protocol.DecodeParams[protocol.SetMCPTokensParams](frame.Params)
			if p.Secrets["ABOX_MCP_GH_TOKEN"] != "mt" {
				t.Errorf("mcp token missing: %v", p.Secrets)
			}
			if _, ok := p.Secrets["XAI_API_KEY"]; ok {
				t.Errorf("model credential leaked to proto-3 guest: %v", p.Secrets)
			}
		default:
			t.Errorf("unexpected method %q", frame.Method)
		}
		ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
		reply(protocol.Frame{ID: frame.ID, Result: ok})
	}
	model := config.Model{Name: "grok-default", Provider: "xai", CredentialEnv: "XAI_API_KEY"}
	err := s.PushSecrets(context.Background(), model, map[string]string{
		"XAI_API_KEY": "mk", "ABOX_MCP_GH_TOKEN": "mt",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPushSecretsProto1Refused(t *testing.T) {
	s, _ := newPipeSandbox(t, 1)
	model := config.Model{Name: "grok-default", Provider: "xai", CredentialEnv: "XAI_API_KEY"}
	err := s.PushSecrets(context.Background(), model, map[string]string{"XAI_API_KEY": "k"})
	if err == nil || !strings.Contains(err.Error(), "make image") {
		t.Fatalf("got %v", err)
	}
}

func TestPushSecretsEmptyNoop(t *testing.T) {
	s, g := newPipeSandbox(t, 1)
	g.onRequest = func(frame protocol.Frame, _ func(protocol.Frame)) {
		t.Errorf("unexpected frame %q", frame.Method)
	}
	model := config.Model{Name: "grok-default", Provider: "xai", CredentialEnv: "XAI_API_KEY"}
	if err := s.PushSecrets(context.Background(), model, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSetModelDropsSecretsOnProto3(t *testing.T) {
	s, g := newPipeSandbox(t, 3)
	g.onRequest = func(frame protocol.Frame, reply func(protocol.Frame)) {
		p, err := protocol.DecodeParams[protocol.SetModelParams](frame.Params)
		if err != nil {
			t.Errorf("params: %v", err)
		}
		if len(p.Secrets) != 0 {
			t.Errorf("proto-3 set_model secrets: %v", p.Secrets)
		}
		ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
		reply(protocol.Frame{ID: frame.ID, Result: ok})
	}
	model := config.Model{Name: "g", Provider: "xai", CredentialEnv: "XAI_API_KEY"}
	if err := s.SetModel(context.Background(), model, map[string]string{"XAI_API_KEY": "k"}); err != nil {
		t.Fatal(err)
	}
}

func TestGuestCallMidTurn(t *testing.T) {
	s, g := newPipeSandbox(t, 3)
	turnStarted := make(chan string, 1)
	gotOpen := make(chan protocol.ProviderOpenResult, 1)
	g.onRequest = func(frame protocol.Frame, reply func(protocol.Frame)) {
		if frame.Method != "user_turn" {
			t.Errorf("unexpected method %q", frame.Method)
			ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
			reply(protocol.Frame{ID: frame.ID, Result: ok})
			return
		}
		go func() {
			open, _ := protocol.EncodeParams(protocol.ProviderOpenParams{Model: "grok-default"})
			g.write(protocol.Frame{V: protocol.Version, ID: "g-1", Method: "provider_open", Params: open})
			turnStarted <- frame.ID
		}()
	}
	g.onResponse = func(frame protocol.Frame) {
		if frame.ID != "g-1" {
			return
		}
		var res protocol.ProviderOpenResult
		if err := json.Unmarshal(frame.Result, &res); err != nil {
			t.Errorf("open result: %v", err)
			return
		}
		gotOpen <- res
	}
	s.OnGuestCall = handlerFunc(func(ctx context.Context, method string, params json.RawMessage, notify func(string, any) error) (any, *protocol.Error) {
		if method != "provider_open" {
			return nil, &protocol.Error{Code: "host", Message: "unexpected " + method}
		}
		return protocol.ProviderOpenResult{StreamID: "s1"}, nil
	})

	done := make(chan error, 1)
	go func() {
		done <- s.UserTurn(context.Background(), "hi", nil)
	}()

	turnID := <-turnStarted
	res := <-gotOpen
	if res.StreamID != "s1" {
		t.Fatalf("open result %+v", res)
	}
	ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
	g.write(protocol.Frame{ID: turnID, Result: ok})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not complete")
	}
}

func TestGuestCallUnknownMethodTypedError(t *testing.T) {
	s, g := newPipeSandbox(t, 3)
	s.startReading() // the test guest speaks before any host call starts the loop
	gotReply := make(chan protocol.Frame, 1)
	g.onRequest = func(frame protocol.Frame, reply func(protocol.Frame)) {
		ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
		reply(protocol.Frame{ID: frame.ID, Result: ok})
	}
	g.onResponse = func(frame protocol.Frame) {
		if frame.ID == "g-9" {
			gotReply <- frame
		}
	}
	s.OnGuestCall = handlerFunc(func(ctx context.Context, method string, _ json.RawMessage, _ func(string, any) error) (any, *protocol.Error) {
		return nil, &protocol.Error{Code: "host", Message: "unknown guest method " + method}
	})

	g.write(protocol.Frame{V: protocol.Version, ID: "g-9", Method: "bogus", Params: []byte(`{}`)})
	select {
	case frame := <-gotReply:
		if frame.Error == nil || !strings.Contains(frame.Error.Message, "unknown guest method bogus") {
			t.Fatalf("frame %+v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reply to unknown guest method")
	}
}

func TestGuestCallRejectedBeforeProtocol3(t *testing.T) {
	s, g := newPipeSandbox(t, 2)
	s.startReading()
	called := make(chan struct{}, 1)
	s.OnGuestCall = handlerFunc(func(ctx context.Context, method string, params json.RawMessage, notify func(string, any) error) (any, *protocol.Error) {
		called <- struct{}{}
		return protocol.ProviderOpenResult{StreamID: "s1"}, nil
	})
	gotReply := make(chan protocol.Frame, 1)
	g.onResponse = func(frame protocol.Frame) {
		if frame.ID == "g-1" {
			gotReply <- frame
		}
	}
	g.write(protocol.Frame{V: 2, ID: "g-1", Method: "provider_open", Params: []byte(`{"model":"x"}`)})
	select {
	case frame := <-gotReply:
		if frame.Error == nil || !strings.Contains(frame.Error.Message, "protocol 3") {
			t.Fatalf("frame %+v", frame)
		}
		if frame.Result != nil {
			t.Fatalf("protocol-2 guest received broker result %s", frame.Result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reply rejecting protocol-2 broker call")
	}
	select {
	case <-called:
		t.Fatal("broker handler ran for protocol-2 guest")
	default:
	}
}

func TestGuestCallNoHandlerErrors(t *testing.T) {
	s, g := newPipeSandbox(t, 3)
	s.startReading()
	s.OnGuestCall = nil
	gotReply := make(chan protocol.Frame, 1)
	g.onResponse = func(frame protocol.Frame) {
		if frame.ID == "g-2" {
			gotReply <- frame
		}
	}
	g.write(protocol.Frame{V: protocol.Version, ID: "g-2", Method: "provider_open", Params: []byte(`{}`)})
	select {
	case frame := <-gotReply:
		if frame.Error == nil || !strings.Contains(frame.Error.Message, "not supported") {
			t.Fatalf("frame %+v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reply without handler")
	}
}

func TestGuestCallConcurrencyReturnsBusy(t *testing.T) {
	s, g := newPipeSandbox(t, 3)
	regularSlots := protocol.MaxGuestCalls - 1
	started := make(chan struct{}, regularSlots)
	release := make(chan struct{})
	s.OnGuestCall = handlerFunc(func(ctx context.Context, method string, params json.RawMessage, notify func(string, any) error) (any, *protocol.Error) {
		if method == "provider_cancel" {
			return map[string]bool{"ok": true}, nil
		}
		started <- struct{}{}
		<-release
		return map[string]bool{"ok": true}, nil
	})
	busy := make(chan protocol.Frame, 1)
	cancelReply := make(chan protocol.Frame, 1)
	g.onResponse = func(frame protocol.Frame) {
		if frame.Error != nil && frame.Error.Code == "busy" {
			busy <- frame
		}
		if frame.ID == "g-cancel" {
			cancelReply <- frame
		}
	}
	s.startReading()
	for i := 0; i < regularSlots; i++ {
		g.write(protocol.Frame{V: protocol.Version, ID: fmt.Sprintf("g-%d", i), Method: "hold", Params: []byte(`{}`)})
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not start")
		}
	}
	g.write(protocol.Frame{V: protocol.Version, ID: "g-cancel", Method: "provider_cancel", Params: []byte(`{"stream_id":"s1"}`)})
	select {
	case frame := <-cancelReply:
		if frame.Error != nil {
			t.Fatalf("provider cancellation was blocked: %+v", frame.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider cancellation was blocked by regular guest calls")
	}
	g.write(protocol.Frame{V: protocol.Version, ID: "g-busy", Method: "hold", Params: []byte(`{}`)})
	select {
	case frame := <-busy:
		if frame.ID != "g-busy" || frame.Error.Code != "busy" {
			t.Fatalf("reply %+v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no typed busy reply")
	}
	close(release)
}

func TestGuestCallContextEndsWithConnection(t *testing.T) {
	s, g := newPipeSandbox(t, 3)
	canceled := make(chan struct{})
	s.OnGuestCall = handlerFunc(func(ctx context.Context, method string, params json.RawMessage, notify func(string, any) error) (any, *protocol.Error) {
		<-ctx.Done()
		close(canceled)
		return nil, &protocol.Error{Code: "canceled", Message: ctx.Err().Error()}
	})
	s.startReading()
	g.write(protocol.Frame{V: protocol.Version, ID: "g-life", Method: "hold", Params: []byte(`{}`)})
	s.failConnection(errors.New("test disconnect"))
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler context survived connection")
	}
}

func TestConnectionFailureWakesPendingCall(t *testing.T) {
	host, guest := net.Pipe()
	t.Cleanup(func() { host.Close(); guest.Close() })
	s := &Sandbox{conn: host, GuestProtocol: 3}
	requestRead := make(chan struct{})
	go func() {
		_, _ = protocol.ReadFrame(guest)
		close(requestRead)
		_ = guest.Close()
	}()
	done := make(chan error, 1)
	go func() {
		done <- s.Call(context.Background(), "wait", map[string]bool{"ok": true}, nil)
	}()
	<-requestRead
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("pending call succeeded after disconnect")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending call was not woken")
	}
}

type handlerFunc func(ctx context.Context, method string, params json.RawMessage, notify func(string, any) error) (any, *protocol.Error)

func (h handlerFunc) Handle(ctx context.Context, method string, params json.RawMessage, notify func(string, any) error) (any, *protocol.Error) {
	return h(ctx, method, params, notify)
}

func TestPushSecretsOnClosedConn(t *testing.T) {
	s, g := newPipeSandbox(t, 2)
	g.onRequest = func(frame protocol.Frame, reply func(protocol.Frame)) {
		ok, _ := protocol.EncodeParams(map[string]bool{"ok": true})
		reply(protocol.Frame{ID: frame.ID, Result: ok})
	}
	g.conn.Close()
	s.conn.Close()
	model := config.Model{Name: "g", CredentialEnv: "XAI_API_KEY"}
	err := s.PushSecrets(context.Background(), model, map[string]string{"XAI_API_KEY": "k"})
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("got %v", err)
	}
}
