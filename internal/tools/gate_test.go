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

func TestGatePendingListsUnresolvedRequests(t *testing.T) {
	g := NewGate()
	g.SetEmitter(func(req ConfirmRequest) {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan Decision, 1)
	go func() {
		done <- g.Request(ctx, ConfirmRequest{ID: "p1", Tool: "bash", Args: `{"command":"ls"}`})
	}()

	deadline := time.After(time.Second)
	for {
		pending := g.Pending()
		if len(pending) == 1 {
			if pending[0].ID != "p1" || pending[0].Tool != "bash" {
				t.Fatalf("pending = %+v", pending)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("pending request never appeared: %+v", pending)
		case <-time.After(10 * time.Millisecond):
		}
	}

	if !g.Resolve("p1", Decision{Allow: true, Remember: RememberNever}) {
		t.Fatal("resolve failed")
	}
	select {
	case d := <-done:
		if !d.Allow {
			t.Fatal("request should be allowed")
		}
	case <-time.After(time.Second):
		t.Fatal("request did not resolve")
	}
	if pending := g.Pending(); len(pending) != 0 {
		t.Fatalf("pending after resolve = %+v", pending)
	}
}

func TestGateRememberMatchesCommandFamily(t *testing.T) {
	g := NewGate()
	g.SetEmitter(func(req ConfirmRequest) {
		g.Resolve(req.ID, Decision{Allow: true, Remember: RememberSession})
	})

	first := ConfirmRequest{
		ID: "q1", Tool: "bash", Args: `{"command":"excelcli query /tmp/a.db --sql SELECT 1"}`,
		MatchKey: "bash:excelcli query",
	}
	if d := g.Request(context.Background(), first); !d.Allow {
		t.Fatal("first request should be allowed")
	}

	called := false
	g.SetEmitter(func(req ConfirmRequest) {
		called = true
	})
	second := ConfirmRequest{
		ID: "q2", Tool: "bash", Args: `{"command":"excelcli query /tmp/a.db --sql SELECT 2"}`,
		MatchKey: "bash:excelcli query",
	}
	if d := g.Request(context.Background(), second); !d.Allow {
		t.Fatal("second request should be allowed via remembered command family")
	}
	if called {
		t.Fatal("emitter should not be called for remembered command family")
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
