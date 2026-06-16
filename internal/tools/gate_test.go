package tools

import (
	"context"
	"testing"
	"time"
)

func TestGateRequestBlocksUntilResolved(t *testing.T) {
	g := NewGate()
	g.SetEmitter(func(req ConfirmRequest) {
		// simulate UI resolving asynchronously
		go func() {
			time.Sleep(20 * time.Millisecond)
			g.Resolve(req.ID, Decision{Allow: true})
		}()
	})

	req := ConfirmRequest{ID: "r1", Tool: "bash", Args: `{"command":"ls"}`}
	done := make(chan Decision, 1)
	go func() {
		d := g.Request(context.Background(), req)
		done <- d
	}()

	select {
	case d := <-done:
		if !d.Allow {
			t.Errorf("want allow=true")
		}
	case <-time.After(time.Second):
		t.Fatal("gate never resolved within 1s")
	}
}

func TestGateDeniedPropagates(t *testing.T) {
	g := NewGate()
	g.SetEmitter(func(req ConfirmRequest) {
		g.Resolve(req.ID, Decision{Allow: false, Remember: RememberNever})
	})
	d := g.Request(context.Background(), ConfirmRequest{ID: "r2", Tool: "bash"})
	if d.Allow {
		t.Errorf("want denied")
	}
}

func TestGateContextCancel(t *testing.T) {
	g := NewGate()
	g.SetEmitter(func(req ConfirmRequest) {}) // never resolves
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	d := g.Request(ctx, ConfirmRequest{ID: "r3"})
	if d.Allow {
		t.Errorf("want denied on ctx cancel")
	}
}

func TestGateRememberAllowsNextTime(t *testing.T) {
	g := NewGate()
	g.SetEmitter(func(req ConfirmRequest) {
		g.Resolve(req.ID, Decision{Allow: true, Remember: RememberSession})
	})
	// first request: goes through emitter, remembered
	d1 := g.Request(context.Background(), ConfirmRequest{ID: "a1", Tool: "file_write", Args: `{"path":"x"}`})
	if !d1.Allow {
		t.Fatal("first should be allowed")
	}
	// second identical request: short-circuited by remember rule, emitter not called
	called := false
	g.SetEmitter(func(req ConfirmRequest) {
		called = true
		g.Resolve(req.ID, Decision{Allow: true})
	})
	d2 := g.Request(context.Background(), ConfirmRequest{ID: "a2", Tool: "file_write", Args: `{"path":"x"}`})
	if !d2.Allow {
		t.Error("second should be allowed via remember")
	}
	if called {
		t.Error("emitter should not be called on a remembered request")
	}
}
