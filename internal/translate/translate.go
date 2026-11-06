// Package translate runs several translation services at once and reports
// their results as they arrive.
package translate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"sotto/internal/config"
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
	ErrNotConfigured = errors.New("service not configured")
	ErrQuota         = errors.New("quota exceeded")
	ErrAuth          = errors.New("invalid credentials")
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

// DefaultTranslators lists the built-in services in default card order.
// Baidu's free tier allows one query a second, so it gets a limiter.
func DefaultTranslators() []Translator {
	return []Translator{
		DeepL{},
		OpenAI{},
		Baidu{Limiter: baiduLimiter()},
		Tencent{},
		Youdao{},
		Azure{},
		Google{},
		Dictionary{},
	}
}

// Run translates req with every enabled and available service, calling
// emit for each result as it arrives. It blocks until all have reported,
// so callers run it on a goroutine of its own.
func Run(ctx context.Context, cfg config.Settings, k KeyStore, req Request, emit func(Result)) {
	RunWith(ctx, DefaultTranslators(), cfg, k, req, emit)
}

// Available lists the enabled services that are ready to run, in card
// order, so the UI can tell a slow service from an unconfigured one.
func Available(cfg config.Settings, k KeyStore) []string {
	var out []string
	for _, t := range orderTranslators(DefaultTranslators(), cfg.Services.Order) {
		if contains(cfg.Services.Enabled, t.Name()) && t.Available(k, cfg) {
			out = append(out, t.Name())
		}
	}
	return out
}

// maxChunkRunes is the largest piece sent in one request. Baidu's free
// endpoint rejects bodies over 6000 bytes (about 2000 Han characters);
// 1800 runes stays under that for every service and still keeps many
// paragraph boundaries intact.
const maxChunkRunes = 1800

// RunWith is Run over a given set of services, for tests.
func RunWith(ctx context.Context, all []Translator, cfg config.Settings, k KeyStore, req Request, emit func(Result)) {
	var wg sync.WaitGroup
	for _, t := range orderTranslators(all, cfg.Services.Order) {
		if !contains(cfg.Services.Enabled, t.Name()) || !t.Available(k, cfg) {
			continue
		}
		t := t
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := translateLong(ctx, t, req, k, cfg)
			if err != nil {
				if errors.Is(err, ErrSkip) {
					return // nothing to add: no card, no error
				}
				res = Result{Service: t.Name(), Err: UserMessage(err)}
			}
			emit(res)
		}()
	}
	wg.Wait()
}

// translateLong sends a long text in sentence/paragraph-sized pieces and
// joins the answers. Uploading a whole article in one request trips the
// body limits of Baidu and the free Google endpoint; splitting keeps the
// text translatable at the cost of a little context.
func translateLong(ctx context.Context, t Translator, req Request, k KeyStore, cfg config.Settings) (Result, error) {
	parts := splitLongText(req.Text, maxChunkRunes)
	if len(parts) <= 1 {
		return t.Translate(ctx, req, k, cfg)
	}
	texts := make([]string, 0, len(parts))
	detected := ""
	for i, part := range parts {
		r := req
		r.Text = part
		res, err := t.Translate(ctx, r, k, cfg)
		if err != nil {
			return Result{}, fmt.Errorf("第 %d/%d 段：%w", i+1, len(parts), err)
		}
		texts = append(texts, res.Text)
		if detected == "" {
			detected = res.DetectedLang
		}
	}
	return Result{
		Service:      t.Name(),
		Text:         strings.Join(texts, "\n"),
		DetectedLang: detected,
	}, nil
}

// splitLongText cuts text at paragraph, sentence or whitespace
// boundaries into pieces of at most limit runes. A single run longer than
// limit is cut hard so progress is always possible.
func splitLongText(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	var out []string
	for start := 0; start < len(runes); {
		end := start + limit
		if end >= len(runes) {
			out = append(out, string(runes[start:]))
			break
		}
		cut := end
		for j := end; j > start; j-- {
			if isChunkBreak(runes[j-1]) {
				cut = j
				break
			}
		}
		if cut == start {
			cut = end
		}
		out = append(out, string(runes[start:cut]))
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
	switch {
	case errors.Is(err, ErrQuota):
		return "本月额度已用完"
	case errors.Is(err, ErrAuth):
		return "凭证无效，请检查设置"
	case errors.Is(err, ErrNotConfigured):
		return "未配置"
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
