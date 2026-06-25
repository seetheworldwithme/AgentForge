package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/agent-rust/core/internal/tools"
	"github.com/creack/pty"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

// TerminalHandler 暴露一个 WebSocket 终端：每个连接在当前工作目录启动一个 PTY shell，
// 双向转发输入/输出并支持 resize。供前端内置终端面板使用。
type TerminalHandler struct {
	WorkDir *tools.WorkDir
}

func (h *TerminalHandler) Routes(r chi.Router) {
	r.Get("/terminal/ws", h.handleWS)
}

// terminalUpgrader 放行所有来源：本应用是本地桌面服务，前端 webview 与 dev 浏览器都可信。
var terminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// ptyMessage 是终端 WebSocket 的 JSON 文本帧协议。
// C→S：input（键盘输入）、resize（尺寸变化）；S→C：output（PTY 输出）、exit（进程退出）。
type ptyMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

func (h *TerminalHandler) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())

	shell := resolveShell()
	cmd := exec.Command(shell[0], shell[1:]...)
	if wd := h.workDir(); wd != "" {
		cmd.Dir = wd
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		cancel()
		_ = ws.WriteJSON(ptyMessage{Type: "exit", Data: "start pty: " + err.Error()})
		ws.Close()
		return
	}
	log.Printf("[Terminal] start shell=%s cwd=%q", shell[0], cmd.Dir)

	var writeMu sync.Mutex // gorilla/websocket 禁止并发写：序列化 output 与 exit 帧
	sendMsg := func(m ptyMessage) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.WriteJSON(m)
	}

	// 统一清理：cancel 唤醒等待方 → 杀子进程 → 关 PTY master（让其读到 EOF）→ 关 WS。
	// 顺序保证两个 pump goroutine 都能退出，避免泄漏。
	defer func() {
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		ptmx.Close()
		ws.Close()
		log.Printf("[Terminal] closed shell=%s", shell[0])
	}()

	// PTY → WS：把子进程输出封装为 output 帧发回前端；读出错（EOF/关闭）即退出。
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				sendMsg(ptyMessage{Type: "output", Data: string(buf[:n])})
			}
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// 子进程退出监听：发 exit 帧通知前端，并 cancel 触发主循环结束。
	go func() {
		_ = cmd.Wait()
		sendMsg(ptyMessage{Type: "exit"})
		cancel()
	}()

	// WS → PTY：主循环阻塞读，分发 input/resize；连接关闭或 ctx 取消即跳出。
loop:
	for {
		_, payload, err := ws.ReadMessage()
		if err != nil {
			break loop
		}
		var msg ptyMessage
		if json.Unmarshal(payload, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "input":
			_, _ = ptmx.Write([]byte(msg.Data))
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: msg.Rows, Cols: msg.Cols})
			}
		}
		select {
		case <-ctx.Done():
			break loop
		default:
		}
	}
}

// resolveShell 选择终端启动的 shell：优先 $SHELL，回退到常见 shell；Windows 用 cmd.exe。
func resolveShell() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe"}
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh}
	}
	for _, cand := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(cand); err == nil {
			return []string{cand}
		}
	}
	return []string{"/bin/sh"}
}

// workDir 返回终端启动目录（nil 安全）。
func (h *TerminalHandler) workDir() string {
	if h.WorkDir != nil {
		return h.WorkDir.Get()
	}
	return ""
}
