// Package command 实现白名单命令的执行，封装为 tool.Tool。
// 安全策略：使用 exec.Command(binary, args...) 结构化传参，不经过 shell。
package command

// CommandSpec 定义一个白名单命令。
type CommandSpec struct {
	Title  string
	Binary string
	Args   []string
}
