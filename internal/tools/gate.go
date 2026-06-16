package tools

import (
	"context"
	"sync"
)

type RememberScope string

const (
	RememberNever   RememberScope = "never"
	RememberSession RememberScope = "session"
	RememberAlways  RememberScope = "always"
)

type ConfirmRequest struct {
	ID   string
	Tool string
	Args string
}

type Decision struct {
	Allow    bool
	Remember RememberScope
}

// Gate mediates dangerous tool actions through human confirmation.
// A tool calls Request() (which blocks); the HTTP layer calls Resolve()
// when the UI posts the user's decision.
type Gate struct {
	mu      sync.Mutex
	pending map[string]chan Decision
	allow   []allowRule // session/always rules added by Resolve(remember)
	emit    func(req ConfirmRequest)
}

type allowRule struct {
	tool         string
	argsContains string
	scope        RememberScope
}

func NewGate() *Gate {
	return &Gate{
		pending: map[string]chan Decision{},
		emit:    func(ConfirmRequest) {}, // no-op default
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
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range g.allow {
		if r.tool == tool && (r.argsContains == "" || containsStr(args, r.argsContains)) {
			return Decision{Allow: true, Remember: r.scope}, true
		}
	}
	return Decision{}, false
}

// Request satisfies GateInterface. It blocks until the UI resolves the
// request, the context is cancelled, or a remember rule short-circuits it.
func (g *Gate) Request(ctx context.Context, req ConfirmRequest) Decision {
	// short-circuit if a remember rule applies
	if d, ok := g.Allowed(req.Tool, req.Args); ok {
		return d
	}
	ch := make(chan Decision, 1)
	g.mu.Lock()
	g.pending[req.ID] = ch
	emit := g.emit
	g.mu.Unlock()

	emit(req)
	defer func() {
		g.mu.Lock()
		delete(g.pending, req.ID)
		g.mu.Unlock()
	}()

	select {
	case d := <-ch:
		if d.Allow && d.Remember != RememberNever {
			g.addRule(req.Tool, req.Args, d.Remember)
		}
		return d
	case <-ctx.Done():
		return Decision{Allow: false, Remember: RememberNever}
	}
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

func (g *Gate) addRule(tool, args string, scope RememberScope) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.allow = append(g.allow, allowRule{tool: tool, argsContains: args, scope: scope})
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
