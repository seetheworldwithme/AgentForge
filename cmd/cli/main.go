// Package main 是 agentforge CLI 入口。
// 组装 internal/* 共享核心，提供 chat / run 子命令。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/agentforge/agentforge/internal/agent"
	"github.com/agentforge/agentforge/internal/conversation"
	"github.com/agentforge/agentforge/internal/llm"
	registrypkg "github.com/agentforge/agentforge/internal/registry"
	"github.com/agentforge/agentforge/internal/tool"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agentforge",
		Short: "AgentForge - 跨平台智能 Agent 工具",
	}
	root.AddCommand(newChatCmd(), newRunCmd())
	return root
}

// config 从环境变量读，缺省回退到配置文件。
// api_key 绝不进命令行参数或日志。
type config struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

func loadConfig() (config, error) {
	cfg := config{
		BaseURL: os.Getenv("AGENTFORGE_BASE_URL"),
		APIKey:  os.Getenv("AGENTFORGE_API_KEY"),
		Model:   os.Getenv("AGENTFORGE_MODEL"),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	// 回退配置文件（api_key 优先用 env）
	path := configPath()
	if data, err := os.ReadFile(path); err == nil {
		var fc config
		if json.Unmarshal(data, &fc) == nil {
			if cfg.BaseURL == "" {
				cfg.BaseURL = fc.BaseURL
			}
			if cfg.APIKey == "" {
				cfg.APIKey = fc.APIKey
			}
			if cfg.Model == "" {
				cfg.Model = fc.Model
			}
		}
	}
	// TODO(secure storage): V2 接入系统 Keychain / 加密文件
	return cfg, nil
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "agentforge", "config.json")
}

func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat [message]",
		Short: "与 Agent 对话（流式输出）",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if cfg.APIKey == "" {
				return fmt.Errorf("api_key 未配置：设置 AGENTFORGE_API_KEY 环境变量或写入 %s", configPath())
			}
			provider := llm.NewOpenAIProvider(cfg.BaseURL, cfg.APIKey, cfg.Model)
			registry := registrypkg.Default()
			mgr := conversation.NewManager()
			a := agent.NewAgent(provider, registry, mgr, agent.Policy{AllowToolCalls: false})

			sink := func(ev agent.LoopEvent) {
				if ev.Kind == agent.LoopDelta {
					fmt.Fprint(os.Stdout, ev.Text)
				}
			}
			return a.Run(context.Background(), args[0], sink)
		},
	}
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <command> [args]",
		Short: "执行白名单命令",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry := registrypkg.Default()
			t, ok := registry.Get(args[0])
			if !ok {
				list := listToolNames(registry)
				return fmt.Errorf("未知命令 %q；可用：%v", args[0], list)
			}
			callArgs := []byte(`{}`)
			if len(args) > 1 {
				callArgs = []byte(args[1])
			}
			events, err := t.Execute(context.Background(), callArgs)
			if err != nil {
				return err
			}
			for ev := range events {
				switch ev.Kind {
				case tool.EventDelta, tool.EventProgress:
					fmt.Fprintln(os.Stdout, ev.Text)
				case tool.EventResult:
					if ev.Result != nil {
						io.WriteString(os.Stdout, ev.Result.Content)
					}
				case tool.EventError:
					fmt.Fprintln(os.Stderr, "error:", ev.Text)
				}
			}
			return nil
		},
	}
}

func listToolNames(r *tool.Registry) []string {
	tools := r.List()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return names
}
