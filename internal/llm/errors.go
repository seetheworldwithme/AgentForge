package llm

import "fmt"

// HTTPError 是 LLM provider 返回的非 2xx HTTP 错误。
// 用结构化类型取代字符串子串匹配，使重试逻辑可用 errors.As 精确判定
// 限流（429）与服务端错误（5xx），避免 provider 改措辞导致漏判。
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("llm http %d: %s", e.StatusCode, e.Body)
}

// RateLimited 判断是否为限流（429）。这类错误在 llm.Retry 中可重试。
func (e *HTTPError) RateLimited() bool {
	return e.StatusCode == 429
}

// ServerError 判断是否为服务端错误（5xx）。
func (e *HTTPError) ServerError() bool {
	return e.StatusCode/100 == 5
}
