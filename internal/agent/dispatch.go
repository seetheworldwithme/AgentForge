package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/tools"
)

// dispatchToolName 是主 agent 用来派生子 agent 的虚拟工具名。它不注册进
// tools.Engine，而是在 agent.Run 的工具执行循环里特判处理——因为只有 agent 自己
// 持有 a.deps.LLM/Tools/Skills/... 全套依赖，普通工具在 Engine 构造时拿不到这些。
const dispatchToolName = "dispatch_agent"

// subResultMaxChars 是子 agent 摘要回传给主 agent 的最大字符数（rune），约 2000
// token，留足主上下文预算；超出尾部截断。
const subResultMaxChars = 8000

// 子 agent 工具调用上限的上下界：取父 MaxToolCalls 的 1/3 后 clamp 到 [5, 30]；
// subToolCallsDefault 是父无上限（MaxToolCalls==0）时子的取值。
const (
	subToolCallsCapFloor = 5
	subToolCallsCapCeil  = 30
	subToolCallsDefault  = 10
)

// subAgentMaxDuration 是单个子 agent 的最长存活时间；实际取 min(它, 父 ctx 剩余)，
// 防止子 agent 吃光父的整段预算。
const subAgentMaxDuration = 10 * time.Minute

// clampSubToolCalls 把父 agent 的工具调用上限收敛为子的上限：取 1/3 并 clamp 到
// [floor, ceil]；父为 0（无上限）时给默认值。抽出为纯函数便于单测。
func clampSubToolCalls(parentMax int) int {
	if parentMax <= 0 {
		return subToolCallsDefault
	}
	return max(subToolCallsCapFloor, min(subToolCallsCapCeil, parentMax/3))
}

// subagentDef 描述一个内置命名子 agent：它的 persona、给主 agent 的选择提示、
// 以及只读工具白名单。白名单是"只读"的硬保证（非 prompt 劝阻）：spec 层不暴露
// 白名单外工具 + execute 层拒绝白名单外调用，双重保险。
type subagentDef struct {
	Type       string   // "explorer" / "reviewer" / "planner"
	Title      string   // 中文名，用于结果前缀与日志
	Desc       string   // 给主 agent：何时该派生这个子 agent
	Prompt     string   // 子 agent 的 persona system prompt（拼进 UserMessage）
	AllowTools []string // 工具白名单（只读工具）；不含 file_write/file_edit/dispatch
}

// explorerPrompt / reviewerPrompt / plannerPrompt 是三个内置子 agent 的 persona。
// 三者都强调：独立上下文、只读、信息密集摘要、给完即停。
const explorerPrompt = `你是一个专注于代码探索的子 agent，运行在独立的上下文窗口中。

职责：在代码库里广撒网搜索、精确定位实现、追踪调用链与依赖关系。
- 用 grep / file_read / bash（仅限只读命令：ls、cat、git log、git grep 等）调研。
- 返回精确的 file:line 引用、关键代码片段、调用关系，便于主 agent 直接定位。
- 绝不修改任何文件；你只有只读工具。
- 任务一完成，立即给出信息密集的发现摘要：找到了什么、在哪（file:line）、关键结论。省略过程叙述与寒暄。
- 给出摘要后立即停止，不要再调用工具。`

const reviewerPrompt = `你是一个严格的代码审查子 agent，运行在独立的上下文窗口中。

职责：审查指定代码，找出 bug、安全漏洞、性能问题、可维护性问题。
- 用 file_read / grep 通读相关代码，基于实际代码下结论，不要臆断。
- 给出分级建议：【必须修复】/【建议修改】/【仅供参考】，每条附理由与位置（file:line）。
- 绝不修改代码；你只有只读工具，只输出审查意见。
- 任务一完成，立即给出结构化审查报告。省略过程叙述与寒暄。
- 给出报告后立即停止，不要再调用工具。`

const plannerPrompt = `你是一个软件架构师子 agent，运行在独立的上下文窗口中。

职责：只读调研后产出可执行的实施计划，不写任何代码改动。
- 用 file_read / grep / bash（仅限只读命令）充分理解现状、依赖与约束。
- 产出结构化计划：目标 / 现状分析 / 改动清单（逐文件，标注新建·修改·删除）/ 实施步骤（每步含验证方式）/ 风险与取舍。
- 绝不修改文件；你只有只读工具。
- 任务一完成，立即给出完整的计划摘要。省略过程叙述与寒暄。
- 给出计划后立即停止，不要再调用工具。`

// builtinSubagents 是内置命名子 agent 表（硬编码）。主 agent 通过 dispatch_agent 的
// subagent_type 参数从中选择。白名单工具均已核实为父 engine 中确实注册的工具
// （file_read/grep/bash；read_skill 未注册故不含；file_write/file_edit 故意排除以保只读）。
var builtinSubagents = map[string]subagentDef{
	"explorer": {
		Type:       "explorer",
		Title:      "探索",
		Desc:       "广撒网搜索/定位代码、追踪调用链（只读，不改文件）",
		Prompt:     explorerPrompt,
		AllowTools: []string{"file_read", "grep", "bash"},
	},
	"reviewer": {
		Type:       "reviewer",
		Title:      "审查",
		Desc:       "代码审查、找 bug/安全/性能问题、给分级建议（只读）",
		Prompt:     reviewerPrompt,
		AllowTools: []string{"file_read", "grep"},
	},
	"planner": {
		Type:       "planner",
		Title:      "规划",
		Desc:       "只读调研后产出结构化实施计划（只读，不改文件）",
		Prompt:     plannerPrompt,
		AllowTools: []string{"file_read", "grep", "bash"},
	},
}

// whitelistEngine 用 tools.NewEngineFromFunc 包装父 engine，只暴露白名单内的工具。
// 不改 tools 包。双重保险：List() 过滤掉白名单外的 spec（模型看不到），Execute()
// 拒绝白名单外的调用（防模型幻觉调用未暴露的工具）。
func whitelistEngine(base *tools.Engine, allow []string) *tools.Engine {
	set := make(map[string]bool, len(allow))
	for _, n := range allow {
		set[n] = true
	}
	return tools.NewEngineFromFunc(
		func() []tools.Spec {
			out := make([]tools.Spec, 0, len(allow))
			for _, s := range base.List() {
				if set[s.Name] {
					out = append(out, s)
				}
			}
			return out
		},
		func(ctx context.Context, name, args string) (tools.Result, error) {
			if !set[name] {
				return tools.Result{Content: "工具 " + name + " 不在该子 agent 的允许列表内", IsError: true}, nil
			}
			return base.Execute(ctx, name, args)
		},
	)
}

// dispatchToolSpec 返回写给主 agent 看的 dispatch_agent 工具规格。Description 列出
// 三个内置子 agent 及其适用场景（对标 opencode describeTask），让主 agent 据此填对
// subagent_type。v1 硬编码固定，本函数无参静态。
func dispatchToolSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: dispatchToolName,
		Description: `派生一个拥有独立上下文窗口的命名子 agent 完成独立子任务，把结果摘要作为本工具返回值。子 agent 看不到主对话历史，所以任务描述必须自包含。

可用子 agent（按 subagent_type 选择）：
- explorer（探索）：广撒网搜索/定位代码、追踪调用链。适合"找出所有调用 X 的地方""定位实现 Y 的代码"等会产生大量搜索输出的调研。
- reviewer（审查）：审查代码、找 bug/安全/性能问题、给分级改进建议。适合"审查这段实现的正确性与风险"。
- planner（规划）：只读调研后产出结构化实施计划。适合"为某改动产出可执行计划"。

何时该派生子 agent（重要）：任务会产生大量搜索/读取输出、会污染主上下文，或需要多轮工具调用且过程细节对主任务不重要时。
何时不该（直接用对应工具）：单步操作（读一个文件、跑一条命令、grep 一次）；需要主对话已有上下文才能判断的子任务。`,
		Parameters: `{
  "type": "object",
  "properties": {
    "subagent_type": {
      "type": "string",
      "enum": ["explorer", "reviewer", "planner"],
      "description": "要派生的子 agent 类型"
    },
    "task": {
      "type": "string",
      "description": "自包含的子任务描述：目标、范围、期望产出。子 agent 无任何背景，需在描述中给齐信息。"
    }
  },
  "required": ["subagent_type", "task"]
}`,
	}
}

// dispatchArgs 是 dispatch_agent 入参的解析结构。
type dispatchArgs struct {
	SubagentType string `json:"subagent_type"`
	Task         string `json:"task"`
}

// subResultCollector 实现 EventEmitter，只收集子 agent 的最终文本（"delta" 事件）。
// 它故意不转发任何事件给父 agent 的 Emit——这是关键正确性约束：
//   - 子的 done 若转发，会覆盖父 streamCollector.lastUsage（handler_chat.go 内）；
//   - 子的 tool_call / tool_result 若转发，会被父 collector 当成主对话工具调用持久化，
//     下一轮续聊时污染主上下文；
//   - 子的 delta 若转发，会把子 agent 的中间文本混进主对话流，前端误以为是主回答。
type subResultCollector struct {
	builder strings.Builder
}

func (c *subResultCollector) Emit(event string, data any) {
	if event == "error" {
		if d, ok := data.(map[string]any); ok {
			if msg, _ := d["message"].(string); msg != "" {
				log.Printf("[SubAgent] error event: %s", msg)
			}
		}
		return
	}
	if event != "delta" {
		return
	}
	if d, ok := data.(map[string]any); ok {
		if t, ok := d["text"].(string); ok {
			c.builder.WriteString(t)
		}
	}
}

// Summary 返回子 agent 累积的最终文本，截断到 subResultMaxChars；空文本返回占位信息。
func (c *subResultCollector) Summary() string {
	s := strings.TrimSpace(c.builder.String())
	if s == "" {
		return "[子 agent 未产生输出，可能因工具上限或超时被中止。]"
	}
	return truncate(s, subResultMaxChars)
}

// handleDispatch 解析 dispatch_agent 入参、路由到对应内置子 agent，在 agent.go 工具
// 执行循环里特判调用。planMode 透传给子 agent，使 plan mode（只读）下子 agent 也只读。
func (a *Agent) handleDispatch(ctx context.Context, rawArgs string, planMode bool) tools.Result {
	var args dispatchArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return tools.Result{
			Content: fmt.Sprintf("dispatch_agent 参数解析失败：%v（args=%s）", err, truncate(rawArgs, 200)),
			IsError: true,
		}
	}
	if args.SubagentType == "" {
		return tools.Result{Content: "dispatch_agent 失败：必须指定 subagent_type（可选：explorer / reviewer / planner）", IsError: true}
	}
	def, ok := builtinSubagents[args.SubagentType]
	if !ok {
		return tools.Result{
			Content: fmt.Sprintf("dispatch_agent 失败：未知 subagent_type %q（可选：explorer / reviewer / planner）", args.SubagentType),
			IsError: true,
		}
	}
	return a.runSubAgent(ctx, args.Task, def, planMode)
}

// runSubAgent 在独立上下文中跑一个命名子 agent（由 def 定义）完成 task，返回带前缀摘要。
//
// 关键点：
//  1. 值拷贝父 deps，但 Tools 替换为 whitelistEngine（只暴露 def.AllowTools 内的工具，
//     保证只读）；MaxToolCalls 收紧到父的 1/3（clamp [5,30]）。
//  2. noDispatch=true：子 agent 的 toolSpecs 不暴露 dispatch_agent，杜绝递归套娃。
//  3. 用独立 collector，绝不把父 Emit 传给子——见 subResultCollector 注释。
//  4. 套独立 deadline（min(subAgentMaxDuration, 父 ctx 剩余)），防子 agent 吃光父预算。
//  5. 空 History（独立上下文），UserMessage=def.Prompt+task，PlanMode 透传（防绕过）。
func (a *Agent) runSubAgent(ctx context.Context, task string, def subagentDef, planMode bool) tools.Result {
	if strings.TrimSpace(task) == "" {
		return tools.Result{Content: "dispatch_agent 失败：task 参数为空", IsError: true}
	}

	// 子 deadline：不超过 subAgentMaxDuration，也不超过父 ctx 剩余。
	deadline := subAgentMaxDuration
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining > 0 && remaining < deadline {
			deadline = remaining
		}
	}
	subCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	// 值拷贝父 deps：Tools 换成白名单 engine（只读），MaxToolCalls 收紧。
	subDeps := a.deps
	subDeps.Tools = whitelistEngine(a.deps.Tools, def.AllowTools)
	subDeps.MaxToolCalls = clampSubToolCalls(a.deps.MaxToolCalls)

	// noDispatch=true：子的 toolSpecs 不暴露 dispatch_agent，杜绝递归。
	sub := &Agent{deps: subDeps, noDispatch: true}

	collector := &subResultCollector{}
	start := time.Now()
	log.Printf("[SubAgent] start type=%s task_len=%d max_tool_calls=%d deadline=%s plan_mode=%t",
		def.Type, len(task), subDeps.MaxToolCalls, deadline, planMode)

	sub.Run(subCtx, RunInput{
		History:      nil, // 独立上下文窗口：子 agent 看不到主对话
		Emit:         collector,
		ToolsEnabled: true,
		UseRAG:       false,
		UserMessage:  def.Prompt + "\n\n---\n\n任务：" + task,
		PlanMode:     planMode, // 透传：plan mode 下子 agent 也只读，防绕过
	})

	summary := collector.Summary()
	log.Printf("[SubAgent] done type=%s duration=%s summary_len=%d",
		def.Type, time.Since(start).Round(time.Millisecond), len(summary))
	// 前缀用 def.Title，让主 agent 明确这是哪类子 agent 的输出。
	return tools.Result{Content: "子代理（" + def.Title + "）执行结果：\n\n" + summary}
}
