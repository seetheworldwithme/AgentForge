package agent

import (
	"context"
	"fmt"

	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	"github.com/agentforge/agentforge/internal/tool"
)

// Agent 持有对话运行时状态，执行 Agent Loop。
type Agent struct {
	llm     llm.Provider
	tools   *tool.Registry
	history *conversation.Manager
	policy  Policy
}

func NewAgent(provider llm.Provider, tools *tool.Registry, history *conversation.Manager, policy Policy) *Agent {
	if tools == nil {
		tools = tool.NewRegistry()
	}
	return &Agent{
		llm:     provider,
		tools:   tools,
		history: history,
		policy:  policy,
	}
}

// Run 执行一次用户输入，流式推送事件直到对话结束。
func (a *Agent) Run(ctx context.Context, userInput string, sink EventSink) error {
	a.history.AppendUser(userInput)

	maxIter := a.policy.effectiveMaxIterations()
	for iter := 0; iter < maxIter; iter++ {
		msgs := a.history.ForRequest()

		var toolDefs []llm.ToolDef
		if a.policy.AllowToolCalls {
			toolDefs = a.buildToolDefs()
		}

		resp, err := a.llm.ChatStream(ctx, llm.Request{
			Messages: msgs,
			Tools:    toolDefs,
			OnDelta: func(text string) {
				if sink != nil {
					sink(DeltaEvent(text))
				}
			},
		})
		if err != nil {
			return fmt.Errorf("agent loop iteration %d: %w", iter, err)
		}

		a.history.AppendAssistant(resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			return nil
		}

		// 有工具调用 → 执行（V2 路径，T9 实现）
		for _, call := range resp.Message.ToolCalls {
			if err := a.executeToolCall(ctx, call, sink); err != nil {
				return err
			}
		}
	}
	if sink != nil {
		sink(LoopEvent{Kind: LoopInfo, Text: "达到最大迭代数"})
	}
	return ErrMaxIterationsReached
}

func (a *Agent) buildToolDefs() []llm.ToolDef {
	tools := a.tools.List()
	defs := make([]llm.ToolDef, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return defs
}

// executeToolCall 桩实现（T9 替换为真实逻辑）。
func (a *Agent) executeToolCall(ctx context.Context, call conversation.ToolCall, sink EventSink) error {
	return fmt.Errorf("tool execution not implemented (V1)")
}
