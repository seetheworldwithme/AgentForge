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

// TestGateAutoAllowSkipsConfirmation 验证"自动"确认模式：开启 autoAllow 后，
// Request 立即放行，不调用 emitter、不进入 pending，用户无需逐次确认。
func TestGateAutoAllowSkipsConfirmation(t *testing.T) {
	g := NewGate()
	called := false
	g.SetEmitter(func(req ConfirmRequest) { called = true })
	g.SetAutoAllow(true)

	d := g.Request(context.Background(), ConfirmRequest{
		ID: "a1", Tool: "bash", Args: `{"command":"rm -rf /tmp/x"}`,
	})
	if !d.Allow {
		t.Fatal("auto-allow should permit every tool call without confirmation")
	}
	if called {
		t.Fatal("emitter must not be called in auto-allow mode")
	}
}

// TestGateManualModeStillConfirms 验证默认（手动）模式下行为不变：emitter
// 被调用，仍走正常确认流程。
func TestGateManualModeStillConfirms(t *testing.T) {
	g := NewGate()
	emitCount := 0
	g.SetEmitter(func(req ConfirmRequest) {
		emitCount++
		g.Resolve(req.ID, Decision{Allow: true, Remember: RememberNever})
	})
	if d := g.Request(context.Background(), ConfirmRequest{ID: "m1", Tool: "bash"}); !d.Allow {
		t.Fatal("manual mode should allow after confirmation")
	}
	if emitCount != 1 {
		t.Fatalf("emitter called %d times in manual mode, want 1", emitCount)
	}
}
