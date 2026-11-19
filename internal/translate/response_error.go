package translate

import (
	"fmt"
	"net/http"
	"regexp"

	"sotto/internal/usermessage"
)

// HTTP error bodies are controlled by the provider or an intermediary. Keep
// the status for unrecognized failures without displaying the response body.
func responseStatusError(service string, status int) error {
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%s：请求过于频繁，请稍后重试", service)
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return fmt.Errorf("%s：请求超时，请稍后重试", service)
	case status >= 500 && status <= 599:
		return fmt.Errorf("%s：服务暂时不可用，请稍后重试", service)
	default:
		return fmt.Errorf("%s：请求失败（HTTP %d）", service, status)
	}
}

var providerCode = regexp.MustCompile(`^(?:-?[0-9]{1,8}|[A-Z][A-Za-z]{1,38}(?:\.[A-Z][A-Za-z]{1,38})?)$`)

func responseCodeError(service, code string) error {
	if providerCode.MatchString(code) {
		return usermessage.New(fmt.Sprintf("%s：翻译失败（错误码 %s）", service, code))
	}
	return fmt.Errorf("%s：翻译失败，请稍后重试", service)
}
