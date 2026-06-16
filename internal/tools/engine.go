package tools

import "context"

// Result is the outcome of a tool execution, returned to the LLM.
type Result struct {
	Content string // text returned to model (may be JSON)
	IsError bool   // true if the tool failed; LLM sees this in content
}

// GateInterface is the minimal surface tools depend on. The concrete Gate
// satisfies it; tests can substitute an auto-allow implementation.
type GateInterface interface {
	Request(ctx context.Context, req ConfirmRequest) Decision
}

// Tool executes one kind of action.
type Tool interface {
	Spec() Spec
	Run(ctx context.Context, args string, gate GateInterface) (Result, error)
}

// Spec describes a tool for the LLM.
type Spec struct {
	Name        string
	Description string
	Parameters  string // JSON schema string
}

// ToolSpec is an alias used outside the package for clarity.
type ToolSpec = Spec

// Registry holds the set of registered tools, keyed by name.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range tools {
		r.tools[t.Spec().Name] = t
	}
	return r
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Spec {
	out := make([]Spec, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Spec())
	}
	return out
}

// Engine ties registry + gate together for the orchestrator.
type Engine struct {
	reg  *Registry
	gate *Gate
	exec func(ctx context.Context, name, args string) (Result, error)
	list func() []Spec
}

func NewEngine(reg *Registry, gate *Gate) *Engine {
	e := &Engine{reg: reg, gate: gate}
	e.exec = func(ctx context.Context, name, args string) (Result, error) {
		t, ok := reg.Get(name)
		if !ok {
			return Result{Content: "unknown tool: " + name, IsError: true}, nil
		}
		return t.Run(ctx, args, gate)
	}
	e.list = reg.List
	return e
}

// NewEngineFromFunc builds an Engine from plain functions, useful in tests
// and when composing custom execution semantics.
func NewEngineFromFunc(listFn func() []Spec, execFn func(ctx context.Context, name, args string) (Result, error)) *Engine {
	return &Engine{reg: &Registry{}, exec: execFn, list: listFn}
}

func (e *Engine) List() []Spec {
	if e.list != nil {
		return e.list()
	}
	return e.reg.List()
}

func (e *Engine) Execute(ctx context.Context, name, args string) (Result, error) {
	if e.exec != nil {
		return e.exec(ctx, name, args)
	}
	t, ok := e.reg.Get(name)
	if !ok {
		return Result{Content: "unknown tool: " + name, IsError: true}, nil
	}
	return t.Run(ctx, args, e.gate)
}

// AutoAllowGate approves everything — for tests and trusted contexts.
type AutoAllowGate struct{}

func NewAutoAllowGate() GateInterface { return AutoAllowGate{} }

func (AutoAllowGate) Request(ctx context.Context, req ConfirmRequest) Decision {
	return Decision{Allow: true, Remember: RememberNever}
}
