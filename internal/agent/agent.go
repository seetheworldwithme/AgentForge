package agent

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/tools"
)

// ragSimilarityThreshold drops retrieved chunks whose cosine similarity is below
// this value before injecting them into the prompt. Based on observation:
// relevant hits land ~0.45 while noise sits ~0.27-0.30, so 0.3 separates them.
// When everything falls below it, nothing is injected (the model answers from
// its own knowledge) instead of polluting the prompt with low-quality excerpts.
const ragSimilarityThreshold float32 = 0.3

type Agent struct {
	deps Deps
}

func New(deps Deps) *Agent {
	if deps.MaxIter <= 0 {
		deps.MaxIter = 20
	}
	return &Agent{deps: deps}
}

// AgentBuilder is a convenience for handlers/tests to construct an Agent
// progressively. Production wiring uses New(Deps{...}) directly in main.
type AgentBuilder struct {
	LLM   llm.LLMClient
	Tools *tools.Engine
	RAG   RAGRetriever
}

func (b AgentBuilder) Build() *Agent {
	return New(Deps{LLM: b.LLM, Tools: b.Tools, RAG: b.RAG})
}

// RunInput is one invocation of the agent loop.
type RunInput struct {
	History      []llm.Message // current conversation (will be appended to)
	Emit         EventEmitter
	ToolsEnabled bool
	UseRAG       bool
	KBID         string // required when UseRAG
	UserMessage  string // the new user turn (appended to History if non-empty)
}

// Run executes the agent loop and emits events.
func (a *Agent) Run(ctx context.Context, in RunInput) {
	history := in.History
	if in.UserMessage != "" {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: in.UserMessage})
	}

	var toolSpecs []llm.ToolSpec
	if in.ToolsEnabled && a.deps.Tools != nil {
		for _, s := range a.deps.Tools.List() {
			toolSpecs = append(toolSpecs, llm.ToolSpec{
				Name: s.Name, Description: s.Description, Parameters: s.Parameters,
			})
		}
	}

	for iter := 0; iter < a.deps.MaxIter; iter++ {
		// Optionally inject RAG context before the model turn. Low-similarity
		// chunks are filtered out; if none clear the threshold, nothing is
		// injected so the model answers from its own knowledge.
		if in.UseRAG && a.deps.RAG != nil && in.KBID != "" {
			query := lastUserText(history)
			chunks, err := a.deps.RAG.Retrieve(ctx, in.KBID, query, 5)
			if err == nil {
				kept := filterRAGChunks(chunks, ragSimilarityThreshold)
				// log the retrieval once per turn (query is stable across tool
				// iterations) so the RAG pipeline can be debugged/tuned.
				if iter == 0 {
					logRAGRetrieval(query, chunks, kept, ragSimilarityThreshold)
				}
				if len(kept) > 0 {
					history = prependRAGContext(history, kept)
				}
			}
		}

		stream, err := a.deps.LLM.ChatStream(ctx, history, toolSpecs)
		if err != nil {
			in.Emit.Emit("error", map[string]any{"message": err.Error()})
			return
		}

		var assistantText strings.Builder
		var toolCalls []llm.ToolCall
		var usage *llm.Usage
		for chunk := range stream {
			if chunk.Text != "" {
				assistantText.WriteString(chunk.Text)
				in.Emit.Emit("delta", map[string]any{"text": chunk.Text})
			}
			if chunk.ToolCall != nil {
				toolCalls = append(toolCalls, *chunk.ToolCall)
				in.Emit.Emit("tool_call", map[string]any{
					"call_id": chunk.ToolCall.ID,
					"tool":    chunk.ToolCall.Name,
					"input":   map[string]any{"raw": chunk.ToolCall.Args},
				})
			}
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			if chunk.Done {
				break
			}
		}

		// record assistant turn
		history = append(history, llm.Message{
			Role: llm.RoleAssistant, Content: assistantText.String(), ToolCalls: toolCalls,
		})

		if len(toolCalls) == 0 {
			// pure text answer; terminate
			in.Emit.Emit("done", map[string]any{"usage": usage})
			return
		}

		// execute each tool, feed result back
		for _, tc := range toolCalls {
			result := tools.Result{Content: "no tools available", IsError: true}
			if a.deps.Tools != nil {
				r, err := a.deps.Tools.Execute(ctx, tc.Name, tc.Args)
				if err != nil {
					result = tools.Result{Content: err.Error(), IsError: true}
				} else {
					result = r
				}
			}
			in.Emit.Emit("tool_result", map[string]any{
				"call_id": tc.ID, "content": result.Content, "is_error": result.IsError,
			})
			history = append(history, llm.Message{
				Role: llm.RoleTool, Content: result.Content, ToolCallID: tc.ID,
			})
		}
	}

	// hit max iterations
	in.Emit.Emit("done", map[string]any{"reason": "max_iterations"})
}

func lastUserText(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleUser {
			return history[i].Content
		}
	}
	return ""
}

// filterRAGChunks keeps chunks at or above the similarity threshold, preserving
// the similarity-descending order returned by the retriever.
func filterRAGChunks(chunks []RetrievedChunk, threshold float32) []RetrievedChunk {
	kept := make([]RetrievedChunk, 0, len(chunks))
	for _, c := range chunks {
		if c.Similarity >= threshold {
			kept = append(kept, c)
		}
	}
	return kept
}

// logRAGRetrieval prints the retrieval pipeline for debugging: every candidate
// with its similarity and INJECT/DROP verdict. Injected chunks are printed in
// full (that is exactly what the model sees in the prompt); dropped ones get a
// short preview, enough to judge why they missed and to tune the threshold.
func logRAGRetrieval(query string, retrieved, kept []RetrievedChunk, threshold float32) {
	log.Printf("[RAG] query=%q threshold=%.2f candidates=%d injected=%d",
		query, threshold, len(retrieved), len(kept))
	keptSet := make(map[string]bool, len(kept))
	for _, c := range kept {
		keptSet[c.ID] = true
	}
	for i, c := range retrieved {
		flag := "DROP"
		text := c.Text
		if keptSet[c.ID] {
			flag = "INJECT"
		} else {
			text = truncate(strings.TrimSpace(c.Text), 100)
		}
		log.Printf("[RAG] #%d [%s] sim=%.3f %s\n%s", i+1, flag, c.Similarity, c.Filename, text)
	}
}

// truncate clips s to at most n runes (unicode-safe) with an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func prependRAGContext(history []llm.Message, chunks []RetrievedChunk) []llm.Message {
	var sb strings.Builder
	sb.WriteString("Answer using the following knowledge base excerpts where relevant:\n\n")
	for i, c := range chunks {
		sb.WriteString("--- excerpt ")
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString(" (")
		sb.WriteString(c.Filename)
		sb.WriteString(", chunk ")
		sb.WriteString(c.ID)
		sb.WriteString(") ---\n")
		sb.WriteString(c.Text)
		sb.WriteString("\n\n")
	}
	system := llm.Message{Role: llm.RoleSystem, Content: sb.String()}
	// merge with existing system message if present, else prepend
	if len(history) > 0 && history[0].Role == llm.RoleSystem {
		merged := history[0]
		merged.Content = sb.String() + "\n" + history[0].Content
		out := make([]llm.Message, len(history))
		copy(out, history)
		out[0] = merged
		return out
	}
	return append([]llm.Message{system}, history...)
}
