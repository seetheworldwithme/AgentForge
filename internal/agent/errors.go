package agent

import "errors"

// ErrMaxIterationsReached 在 Loop 达到 MaxIterations 上限仍未结束时返回。
var ErrMaxIterationsReached = errors.New("agent: max iterations reached")
