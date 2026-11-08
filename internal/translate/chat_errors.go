package translate

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const maxChatErrorBytes = 16 << 10

// chatResponseError maps documented codes before HTTP status: a 429 may mean
// exhausted credits rather than a temporary rate limit. Never echo messages,
// which compatible gateways may fill with credentials, input text, or HTML.
func chatResponseError(service string, status int, anonymous bool, body []byte) error {
	message := ""
	if anonymous {
		// Anonymous users cannot fix the gateway's model, billing, or credentials.
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			message = "暂时无法匿名访问，请稍后重试或切换翻译服务"
		}
	} else if len(body) <= maxChatErrorBytes {
		var envelope struct{ Error json.RawMessage }
		if json.Unmarshal(body, &envelope) == nil && len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			body = envelope.Error
		}
		var detail struct {
			Code json.RawMessage
			Type string
		}
		if json.Unmarshal(body, &detail) == nil {
			var code string
			if json.Unmarshal(detail.Code, &code) != nil {
				code = string(detail.Code) // SiliconFlow also returns numeric codes.
			}
			switch service {
			case "智谱":
				// https://docs.bigmodel.cn/cn/api/api-code
				switch code {
				case "1000", "1001", "1003":
					return fmt.Errorf("%s：%w", service, ErrAuth)
				case "1113":
					message = "账户余额不足，请前往控制台充值"
				case "1211":
					message = "模型不存在，请检查模型名称"
				case "1220":
					message = "无调用权限，请检查服务配置"
				case "1261":
					message = "原文过长，请缩短后重试"
				case "1301":
					message = "内容被服务拦截，请修改原文后重试"
				case "1302":
					message = "请求过于频繁，请稍后重试"
				case "1305":
					message = "服务暂时不可用，请稍后重试"
				case "1308", "1310":
					message = "调用额度已用完，请检查服务商控制台"
				}
			case "硅基流动":
				// https://docs.siliconflow.cn/docs/userguide/faqs/error-code
				if code == "20012" {
					message = "模型不存在，请检查模型名称"
				}
			case "OpenAI":
				// https://developers.openai.com/api/docs/guides/error-codes
				// Specific billing codes take priority over insufficient_quota type.
				switch code {
				case "invalid_api_key":
					return fmt.Errorf("%s：%w", service, ErrAuth)
				case "model_not_found":
					message = "模型不可用，请检查模型名称与调用权限"
				case "context_length_exceeded":
					message = "原文过长，请缩短后重试"
				case "credit_balance_exhausted":
					message = "账户余额不足，请前往控制台充值"
				case "insufficient_quota", "organization_spend_limit_exceeded", "project_spend_limit_exceeded", "organization_usage_limit_exceeded":
					message = "调用额度已用完，请检查服务商控制台"
				case "rate_limit_exceeded", "slow_down":
					message = "请求过于频繁，请稍后重试"
				case "server_is_overloaded":
					message = "服务暂时不可用，请稍后重试"
				}
				if message == "" && detail.Type == "insufficient_quota" {
					message = "调用额度已用完，请检查服务商控制台"
				}
			}
		}
	}
	if message == "" {
		switch {
		case status == http.StatusUnauthorized:
			return fmt.Errorf("%s：%w", service, ErrAuth)
		case status == http.StatusPaymentRequired && !anonymous:
			message = "账户余额不足，请前往控制台充值"
		case status == http.StatusForbidden:
			message = "无调用权限，请检查服务配置"
		case status == http.StatusTooManyRequests:
			message = "请求过于频繁，请稍后重试"
		case status >= http.StatusInternalServerError:
			message = "服务暂时不可用，请稍后重试"
		case status == http.StatusOK:
			message = "生成中断，请重试"
		default:
			message = fmt.Sprintf("请求失败（HTTP %d）", status)
		}
	}
	return fmt.Errorf("%s：%s", service, message)
}
