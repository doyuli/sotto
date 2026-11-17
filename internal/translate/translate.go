// Package translate runs several translation services at once and reports
// their results as they arrive.
package translate

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// Request is one translation to fan out to every enabled service.
type Request struct {
	Text       string
	SourceLang string // app-internal code, "auto" to detect
	TargetLang string // app-internal code
	// SingleWord marks a lone English word, which the dictionary
	// service also answers; everything else it skips.
	SingleWord bool
}

// Result is one service's answer. Err carries a user-facing message from
// UserMessage when the service failed.
type Result struct {
	Streaming    bool   `json:"streaming"` // true until the terminal result
	Service      string `json:"service"`
	Text         string `json:"text"`
	DetectedLang string `json:"detectedLang"`
	Err          string `json:"err"`
}

// KeyStore reads service credentials, by the names in internal/keys.
type KeyStore interface {
	Get(name string) string
}

// Errors the bubble shows a message for; see UserMessage.
var (
	ErrNotConfigured     = errors.New("service not configured")
	ErrQuota             = errors.New("quota exceeded")
	ErrAuth              = errors.New("invalid credentials")
	ErrAccess            = errors.New("service access denied")
	ErrGenerationTimeout = errors.New("translation generation timed out")
	// ErrSkip: this service has nothing to add to the request — the
	// fan-out leaves it off the bubble instead of showing an error.
	ErrSkip = errors.New("service skipped")
)

// Translator is one translation service.
type Translator interface {
	Name() string
	Available(k KeyStore, cfg config.Settings) bool
	Translate(ctx context.Context, req Request, k KeyStore, cfg config.Settings) (Result, error)
}

// StreamingTranslator optionally publishes cumulative text before completion.
// Callbacks run synchronously; the returned result settles success or failure.
type StreamingTranslator interface {
	Stream(context.Context, Request, KeyStore, config.Settings, func(Result)) (Result, error)
}

// DefaultTranslators lists the built-in services in default card order.
// Baidu's free tier allows one query a second, so it gets a limiter.
func DefaultTranslators() []Translator {
	return []Translator{
		IndexTranslate{},
		Google{},
		SiliconFlow{},
		Zhipu{},
		Baidu{Limiter: baiduLimiter},
		Volcengine{},
		DeepL{},
		OpenAI{},
		Tencent{},
		Youdao{},
		Azure{},
		Dictionary{},
	}
}

// Run translates req with every enabled and available service, calling
// emit for each result as it arrives. It blocks until all have reported,
// so callers run it on a goroutine of its own.
func Run(ctx context.Context, cfg config.Settings, k KeyStore, req Request, emit func(Result)) {
	RunWith(ctx, DefaultTranslators(), cfg, k, req, emit)
}

// Configured includes services the user can turn on with the current credentials.
func Configured(cfg config.Settings, k KeyStore) []string {
	out := []string{}
	for _, t := range DefaultTranslators() {
		if t.Available(k, cfg) {
			out = append(out, t.Name())
		}
	}
	return out
}

// EnabledProviders follows the configured card order, excluding the dictionary.
func EnabledProviders(settings config.ServiceSettings) []Translator {
	return slices.DeleteFunc(orderTranslators(DefaultTranslators(), settings.Order), func(service Translator) bool {
		return service.Name() == "dictionary" || !contains(settings.Enabled, service.Name())
	})
}

// Test checks a fixed, non-streaming translation, including disabled services.
// A successful probe does not establish streaming support.
func Test(ctx context.Context, name string, cfg config.Settings, k KeyStore) error {
	for _, t := range DefaultTranslators() {
		if t.Name() == name {
			if err := credentialError(k, name); err != nil {
				return err
			}
			if !t.Available(k, cfg) {
				return ErrNotConfigured
			}
			_, err := t.Translate(ctx, Request{Text: "Hello world", SourceLang: "en", TargetLang: "zh-HANS"}, k, cfg)
			return err
		}
	}
	return fmt.Errorf("未知的翻译服务")
}

// maxChunkRunes is the largest piece sent in one request. Baidu's free
// endpoint rejects bodies over 6000 bytes (about 2000 Han characters);
// 1800 runes stays under that for every service and still keeps many
// paragraph boundaries intact.
const maxChunkRunes = 1800

// RunWith is Run over a given set of services, for tests.
func RunWith(ctx context.Context, all []Translator, cfg config.Settings, k KeyStore, req Request, emit func(Result)) {
	parts, inputErr := prepareParts(req.Text)
	var wg sync.WaitGroup
	for _, t := range orderTranslators(all, cfg.Services.Order) {
		if !contains(cfg.Services.Enabled, t.Name()) {
			continue
		}
		if inputErr != nil {
			if ctx.Err() == nil {
				emit(Result{Service: t.Name(), Err: UserMessage(inputErr)})
			}
			continue
		}
		if err := credentialError(k, t.Name()); err != nil {
			if ctx.Err() == nil {
				emit(Result{Service: t.Name(), Err: UserMessage(err)})
			}
			continue
		}
		if !t.Available(k, cfg) {
			if ctx.Err() == nil {
				emit(Result{Service: t.Name(), Err: UserMessage(ErrNotConfigured)})
			}
			continue
		}
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			progress, stopProgress := streamProgressWithPrefix(ctx, emit)
			res, err := translateParts(ctx, t, req, k, cfg, parts, progress)
			// Stop scheduled partials before the terminal result can enable actions.
			stopProgress()
			if err != nil {
				if errors.Is(err, ErrSkip) {
					return // nothing to add: no card, no error
				}
				res.Service, res.Err, res.Streaming = t.Name(), UserMessage(err), false
			}
			if ctx.Err() == nil {
				emit(res)
			}
		}()
	}
	wg.Wait()
}

// streamProgressWithPrefix publishes the first text immediately and coalesces later
// snapshots at 25 Hz. A timer flushes a burst even if the server then pauses.
func streamProgressWithPrefix(ctx context.Context, emit func(Result)) (func(string, Result), func()) {
	const interval = 40 * time.Millisecond
	var mu sync.Mutex
	var timer *time.Timer
	var last time.Time
	var pending Result
	var prefix string
	stopped := false
	send := func() {
		if !stopped && ctx.Err() == nil {
			snapshot := pending
			snapshot.Text = prefix + pending.Text
			emit(snapshot)
			last = time.Now()
		}
	}
	progress := func(completed string, res Result) {
		if res.Text == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if stopped || ctx.Err() != nil {
			return
		}
		pending, prefix = res, completed
		if timer != nil {
			return
		}
		if last.IsZero() || time.Since(last) >= interval {
			send()
			return
		}
		timer = time.AfterFunc(interval-time.Since(last), func() {
			mu.Lock()
			defer mu.Unlock()
			send()
			timer = nil
		})
	}
	stop := func() {
		mu.Lock()
		defer mu.Unlock()
		stopped = true
		if timer != nil {
			timer.Stop()
		}
	}
	return progress, stop
}

// translateParts sends the shared source chunks sequentially and joins their
// results, preserving completed output when a later request fails.
func translateParts(ctx context.Context, t Translator, req Request, k KeyStore, cfg config.Settings, parts []string, emit func(string, Result)) (Result, error) {
	var completed strings.Builder
	result := Result{Service: t.Name()}
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		r := req
		r.Text = part
		prefix := completed.String()
		if prefix != "" {
			prefix += "\n"
		}
		chunkCtx, cancel := context.WithCancel(ctx)
		var res Result
		var err error
		var oversized bool
		var lastSafe string
		if streaming, ok := t.(StreamingTranslator); ok {
			res, err = streaming.Stream(chunkCtx, r, k, cfg, func(partial Result) {
				if oversized || ctx.Err() != nil {
					return
				}
				if len(partial.Text) > maxResultBytes-len(prefix) {
					oversized = true
					cancel()
					return
				}
				lastSafe = partial.Text
				partial.Service, partial.Streaming = t.Name(), true
				if emit != nil {
					emit(prefix, partial)
				}
			})
		} else {
			res, err = t.Translate(chunkCtx, r, k, cfg)
		}
		cancel()
		if oversized || len(res.Text) > maxResultBytes-len(prefix) {
			if lastSafe != "" {
				result.Text = prefix + lastSafe
			}
			return result, ErrResultLimit
		}
		if res.Text != "" {
			if completed.Len() != 0 {
				completed.WriteByte('\n')
			}
			completed.WriteString(res.Text)
		}
		if result.DetectedLang == "" {
			result.DetectedLang = res.DetectedLang
		}
		result.Text = completed.String()
		if err != nil {
			if len(parts) > 1 {
				err = fmt.Errorf("第 %d/%d 段：%w", i+1, len(parts), err)
			}
			return result, err
		}
	}
	return result, nil
}

// splitLongText cuts text at paragraph, sentence or whitespace
// boundaries into pieces of at most limit runes. A single run longer than
// limit is cut hard so progress is always possible.
func splitLongText(text string, limit int) []string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	var out []string
	for start := 0; start < len(text); {
		end, cut := start, start
		for count := 0; end < len(text) && count < limit; count++ {
			r, size := utf8.DecodeRuneInString(text[end:])
			end += size
			if isChunkBreak(r) {
				cut = end
			}
		}
		if end == len(text) || cut == start {
			cut = end
		}
		out = append(out, text[start:cut])
		start = cut
	}
	return out
}

// isChunkBreak reports runes a chunk may end on.
func isChunkBreak(r rune) bool {
	switch r {
	case '\n', '。', '！', '？', '；', '：', '，', '、',
		'.', '!', '?', ';', ':', ',':
		return true
	}
	return unicode.IsSpace(r)
}

// orderTranslators puts the services in the card order, appending any the
// order does not name.
func orderTranslators(all []Translator, order []string) []Translator {
	out := make([]Translator, 0, len(all))
	for _, name := range order {
		for _, t := range all {
			if t.Name() == name {
				out = append(out, t)
				break
			}
		}
	}
	for _, t := range all {
		if !contains(order, t.Name()) {
			out = append(out, t)
		}
	}
	return out
}

// UserMessage turns a service error into the message the bubble shows.
func UserMessage(err error) string {
	var networkErr *url.Error
	switch {
	case errors.Is(err, ErrEmptySource):
		return "清理后的原文为空，请修改原文或调整命名转换设置。"
	case errors.Is(err, ErrInputLimit):
		return "原文过长，单次最多翻译 18000 个字符，请分次提交"
	case errors.Is(err, ErrResultLimit):
		return "译文过长，已停止生成；请缩短原文后重试"
	case errors.Is(err, keys.ErrUnavailable):
		return "系统凭据暂时无法读取，请解锁钥匙串或检查系统凭据后重试"
	case errors.Is(err, ErrQuota):
		return "本月额度已用完"
	case errors.Is(err, ErrAuth):
		return "凭证无效，请检查设置"
	case errors.Is(err, ErrAccess):
		return "请在服务商控制台开通翻译服务并检查调用权限"
	case errors.Is(err, ErrNotConfigured):
		return "未配置"
	case errors.Is(err, ErrGenerationTimeout):
		return "生成超时，译文未完成，请重试"
	case errors.Is(err, context.DeadlineExceeded):
		return "连接超时，请稍后重试"
	case errors.As(err, &networkErr):
		return "网络连接失败，请检查网络后重试"
	case err == nil:
		return ""
	default:
		return err.Error()
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// unsupported reports a language the service cannot translate to.
func unsupported(service, code string) error {
	return fmt.Errorf("%s: 不支持的目标语言 %q", service, code)
}

func credentialError(k KeyStore, service string) error {
	names := keys.ForServices(service)
	if reader, ok := k.(interface{ Err(...string) error }); ok && len(names) != 0 {
		return reader.Err(names...)
	}
	return nil
}

// WithoutCredential only disables services requiring this explicitly deleted
// credential. It never reads unrelated entries from the system keyring.
func WithoutCredential(cfg config.Settings, name string) []string {
	var enabled []string
	for _, t := range orderTranslators(DefaultTranslators(), cfg.Services.Order) {
		if contains(cfg.Services.Enabled, t.Name()) && t.Available(deletedCredential(name), cfg) {
			enabled = append(enabled, t.Name())
		}
	}
	return enabled
}

type deletedCredential string

func (d deletedCredential) Get(name string) string {
	if name == string(d) {
		return ""
	}
	return "present"
}
