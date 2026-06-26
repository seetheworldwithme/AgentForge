package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/tools"
)

// askUserToolName 是模型在拿不准、需要用户拍板时调用的虚拟工具名。与 dispatch_agent
// 一样不注册进 tools.Engine，而在 agent.Run 的工具执行循环里特判处理——因为它需要
// agent 持有的 a.deps.Asker（普通工具在 Engine 构造时拿不到）。
const askUserToolName = "ask_user"

// askUserToolSpec 返回写给模型看的 ask_user 工具规格。Description 内嵌「何时该问 /
// 不该问」的判断准则，对标 Claude Code 的 AskUserQuestion——硬约束靠工具描述，避免
// 模型滥用（动辄反问用户）。
func askUserToolSpec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: askUserToolName,
		Description: `当你遇到必须由用户拍板、且无法从上下文或合理默认值推断的决策时，调用本工具向用户提问并给出 2~4 个选项；用户作答后你会收到其选择，再继续任务。

何时该用（需同时满足）：
- 你被卡住，不问就无法正确继续；
- 答案会改变你接下来做什么；
- 没有显而易见的合理默认值；
- 不是你能通过读文件 / grep / 执行命令自行查证的事实。

何时不该用（直接做，不要问）：
- 有合理默认值时：直接采用默认，在回答里说明你的选择，然后继续；
- 能自己读文件、grep、跑命令确认的：自己去查，不要拿去问用户；
- 简单、单一、明确的任务：直接执行。

选项要求：
- 给 2~4 个，彼此互斥，覆盖主要可能性；
- 每个选项给简短的 label 与一行 description（说明其含义或权衡）；
- 用户始终可填「其他」自定义作答，无需你提供该选项。`,
		Parameters: `{
  "type": "object",
  "properties": {
    "question": {
      "type": "string",
      "description": "要问用户的问题，清晰、具体，以问号结尾。"
    },
    "options": {
      "type": "array",
      "minItems": 2,
      "maxItems": 4,
      "items": {
        "type": "object",
        "properties": {
          "label": { "type": "string", "description": "选项名，简短（约 1~8 字）" },
          "description": { "type": "string", "description": "该选项的含义或权衡，一行" }
        },
        "required": ["label"]
      }
    }
  },
  "required": ["question", "options"]
}`,
	}
}

// askUserArgs 是 ask_user 入参的解析结构。
type askUserArgs struct {
	Question string         `json:"question"`
	Options  []askUserOption `json:"options"`
}

type askUserOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// handleAskUser 解析 ask_user 入参、校验后通过 Asker 向用户提问，把回答格式化为
// tool_result。callID 用作 Question.ID，前端据此把回答回传到本次阻塞调用。
// planMode 参数仅为与 handleDispatch 对称保留：ask_user 是只读澄清，plan mode 下
// 同样可用，此处不受 planMode 影响。
func (a *Agent) handleAskUser(ctx context.Context, callID, rawArgs string) tools.Result {
	var args askUserArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return tools.Result{
			Content: fmt.Sprintf("ask_user 参数解析失败：%v（args=%s）", err, truncate(rawArgs, 200)),
			IsError: true,
		}
	}
	if strings.TrimSpace(args.Question) == "" {
		return tools.Result{Content: "ask_user 失败：question 不能为空", IsError: true}
	}
	if len(args.Options) < 2 || len(args.Options) > 4 {
		return tools.Result{Content: fmt.Sprintf("ask_user 失败：options 需要 2~4 个，实际 %d 个", len(args.Options)), IsError: true}
	}
	// 收敛选项：去掉 label 为空的项；label 去重（重复会让用户困惑、回传歧义）。
	opts := make([]tools.QuestionOption, 0, len(args.Options))
	seen := make(map[string]bool, len(args.Options))
	for _, o := range args.Options {
		label := strings.TrimSpace(o.Label)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		opts = append(opts, tools.QuestionOption{Label: label, Description: strings.TrimSpace(o.Description)})
	}
	if len(opts) < 2 {
		return tools.Result{Content: "ask_user 失败：有效选项（label 非空且不重复）不足 2 个", IsError: true}
	}

	ans := a.deps.Asker.Ask(ctx, tools.Question{ID: callID, Text: args.Question, Options: opts})
	switch {
	case ans.Canceled:
		return tools.Result{Content: "用户取消了本次提问。请改用合理默认值继续，或说明你无法在此环节决策。"}
	case strings.TrimSpace(ans.Other) != "":
		return tools.Result{Content: "用户选择（其他）：" + strings.TrimSpace(ans.Other)}
	case strings.TrimSpace(ans.Selection) != "":
		return tools.Result{Content: "用户选择：" + strings.TrimSpace(ans.Selection)}
	default:
		// 用户确认了但既未选选项也未填「其他」：视为未作答，提示模型自行采用默认。
		return tools.Result{Content: "用户未做出明确选择。请改用合理默认值继续，并在回答中说明。"}
	}
}
