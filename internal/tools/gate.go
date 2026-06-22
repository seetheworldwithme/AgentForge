package tools

import (
	"context"
	"log"
	"sync"
	"time"
)

type RememberScope string

const (
	RememberNever   RememberScope = "never"
	RememberSession RememberScope = "session"
	RememberAlways  RememberScope = "always"
)

type ConfirmRequest struct {
	ID           string
	Tool         string
	Args         string
	MatchKey     string
	MatchKeyHint string
}

type Decision struct {
	Allow    bool
	Remember RememberScope
}

// Gate mediates dangerous tool actions through human confirmation.
// A tool calls Request() (which blocks); the HTTP layer calls Resolve()
// when the UI posts the user's decision.
type Gate struct {
	mu          sync.Mutex
	pending     map[string]chan Decision
	pendingReqs map[string]ConfirmRequest
	allow       []allowRule // session/always rules added by Resolve(remember)
	emit        func(req ConfirmRequest)
}

type allowRule struct {
	tool         string
	argsContains string
	matchKey     string
	scope        RememberScope
}

func NewGate() *Gate {
	return &Gate{
		pending:     map[string]chan Decision{},
		pendingReqs: map[string]ConfirmRequest{},
		emit:        func(ConfirmRequest) {}, // no-op default
	}
}

// SetEmitter installs the function called when confirmation is needed.
func (g *Gate) SetEmitter(f func(ConfirmRequest)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.emit = f
}

// Allowed returns a pre-cached decision if args match a remember rule.
func (g *Gate) Allowed(tool, args string) (Decision, bool) {
	return g.allowed(tool, args, "")
}

func (g *Gate) allowed(tool, args, matchKey string) (Decision, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range g.allow {
		if r.tool != tool {
			continue
		}
		if r.matchKey != "" && matchKey == r.matchKey {
			return Decision{Allow: true, Remember: r.scope}, true
		}
		if r.matchKey == "" && (r.argsContains == "" || containsStr(args, r.argsContains)) {
			return Decision{Allow: true, Remember: r.scope}, true
		}
	}
	return Decision{}, false
}

// Request satisfies GateInterface. It blocks until the UI resolves the
// request, the context is cancelled, or a remember rule short-circuits it.
func (g *Gate) Request(ctx context.Context, req ConfirmRequest) Decision {
	// short-circuit if a remember rule applies
	if d, ok := g.allowed(req.Tool, req.Args, req.MatchKey); ok {
		log.Printf("[Gate] remembered tool=%s match_key=%q args=%q allow=%t", req.Tool, req.MatchKey, preview(req.Args, 200), d.Allow)
		return d
	}
	ch := make(chan Decision, 1)
	g.mu.Lock()
	g.pending[req.ID] = ch
	g.pendingReqs[req.ID] = req
	emit := g.emit
	g.mu.Unlock()

	start := time.Now()
	log.Printf("[Gate] wait id=%s tool=%s args=%q", req.ID, req.Tool, preview(req.Args, 200))
	emit(req)
	defer func() {
		g.mu.Lock()
		delete(g.pending, req.ID)
		delete(g.pendingReqs, req.ID)
		g.mu.Unlock()
	}()

	select {
	case d := <-ch:
		if d.Allow && d.Remember != RememberNever {
			g.addRule(req.Tool, req.Args, req.MatchKey, d.Remember)
		}
		log.Printf("[Gate] resolved id=%s tool=%s allow=%t remember=%s duration=%s",
			req.ID, req.Tool, d.Allow, d.Remember, time.Since(start).Round(time.Millisecond))
		return d
	case <-ctx.Done():
		log.Printf("[Gate] canceled id=%s tool=%s duration=%s err=%v",
			req.ID, req.Tool, time.Since(start).Round(time.Millisecond), ctx.Err())
		return Decision{Allow: false, Remember: RememberNever}
	}
}

func (g *Gate) Pending() []ConfirmRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]ConfirmRequest, 0, len(g.pendingReqs))
	for _, req := range g.pendingReqs {
		out = append(out, req)
	}
	return out
}

func (g *Gate) Resolve(id string, d Decision) bool {
	g.mu.Lock()
	ch, ok := g.pending[id]
	g.mu.Unlock()
	if !ok {
		return false
	}
	ch <- d
	return true
}

func (g *Gate) addRule(tool, args, matchKey string, scope RememberScope) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.allow = append(g.allow, allowRule{tool: tool, argsContains: args, matchKey: matchKey, scope: scope})
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func preview(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
