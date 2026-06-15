// Package registry 集中注册所有内置工具，打破 tool↔command 循环依赖。
package registry

import (
	"github.com/agentforge/agentforge/internal/command"
	"github.com/agentforge/agentforge/internal/tool"
)

// Setup 把所有内置工具注册到给定 Registry。
func Setup(r *tool.Registry) {
	r.Register(command.NewSystemInfoTool())
}

// Default 返回已注册好内置工具的全局 Registry。
func Default() *tool.Registry {
	r := tool.NewRegistry()
	Setup(r)
	return r
}
