package llm

import (
	"sort"

	"github.com/agentforge/agentforge/internal/conversation"
)

type deltaChunk struct {
	Index         int
	ID            string
	FunctionName  string
	ArgumentsFrag string
}

type toolCallAccumulator struct {
	calls map[int]*conversation.ToolCall
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{calls: make(map[int]*conversation.ToolCall)}
}

func (a *toolCallAccumulator) add(d deltaChunk) {
	call, exists := a.calls[d.Index]
	if !exists {
		call = &conversation.ToolCall{}
		a.calls[d.Index] = call
	}
	if d.ID != "" {
		call.ID = d.ID
	}
	if d.FunctionName != "" {
		call.Name = d.FunctionName
	}
	if d.ArgumentsFrag != "" {
		call.Args = append(call.Args, []byte(d.ArgumentsFrag)...)
	}
}

func (a *toolCallAccumulator) result() []expectedCall {
	if len(a.calls) == 0 {
		return nil
	}
	indices := make([]int, 0, len(a.calls))
	for i := range a.calls {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	result := make([]expectedCall, 0, len(indices))
	for _, idx := range indices {
		call := a.calls[idx]
		result = append(result, expectedCall{
			ID:   call.ID,
			Name: call.Name,
			Args: string(call.Args),
		})
	}
	return result
}

type expectedCall struct {
	ID   string
	Name string
	Args string
}
