package tools

import "sync"

// WorkDir is a process-wide, thread-safe holder for the current working
// directory. Bash (and other filesystem tools) read it at execution time so
// the user can switch the project root on the fly via the UI without restart.
type WorkDir struct {
	mu sync.RWMutex
	v  string
}

func NewWorkDir() *WorkDir { return &WorkDir{} }

func (w *WorkDir) Get() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.v
}

func (w *WorkDir) Set(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.v = dir
}
