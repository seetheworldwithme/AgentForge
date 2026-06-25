package builtin

import (
	"context"

	"github.com/agent-rust/core/internal/tools"
)

// SkillContentProvider 是 read_skill 工具访问 skills 的最小接口（依赖倒置，避免
// builtin 包直接依赖 skills 包造成循环）。*skills.Manager 隐式实现它。
type SkillContentProvider interface {
	GetSkillContent(id string) (string, error)
}

// ReadSkill 让模型按 skill id 按需加载某个 skill 的 SKILL.md 全文。配合 agent 注入的
// 精简索引使用：常驻 prompt 只放索引，模型判断需要某 skill 时调用本工具拉取全文再执行。
// 纯只读，免用户确认（参考 file_read/grep）。
type ReadSkill struct {
	Skills SkillContentProvider
}

func (ReadSkill) Spec() tools.Spec {
	return tools.Spec{
		Name:        "read_skill",
		Description: "Load the full SKILL.md instructions of a skill by its id. Call this when the user's request matches a skill listed in the skill index, before following that skill.",
		Parameters: `{"type":"object","properties":{
			"skill_id":{"type":"string","description":"skill id from the index, e.g. global:docx"}
		},"required":["skill_id"]}`,
	}
}

func (r ReadSkill) Run(ctx context.Context, args string, gate tools.GateInterface) (tools.Result, error) {
	var p struct {
		SkillID string `json:"skill_id"`
	}
	if err := jsonUnmarshal(args, &p); err != nil {
		return tools.Result{Content: "bad args: " + err.Error(), IsError: true}, nil
	}
	if r.Skills == nil {
		return tools.Result{Content: "skills not available", IsError: true}, nil
	}
	content, err := r.Skills.GetSkillContent(p.SkillID)
	if err != nil {
		return tools.Result{Content: "read_skill error: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: content}, nil
}
