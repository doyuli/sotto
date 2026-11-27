package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"sotto/internal/config"
	"sotto/internal/usermessage"
)

const (
	magpieDefaultPort = 3425
	magpieClientID    = "magpie-sotto"
)

var magpieHTTPClient = &http.Client{
	Transport: defaultChatClient.Transport,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		// Both discovery and translation must stay on the verified local port.
		return http.ErrUseLastResponse
	},
}

type MagpieGateway struct {
	Port                int
	Models              []string
	supportsNoReasoning map[string]bool
}

func (g MagpieGateway) SelectModel(preferred string) (string, error) {
	switch {
	case len(g.Models) == 0:
		return "", usermessage.New("请先在 Magpie 添加可用的模型")
	case preferred != "" && !slices.Contains(g.Models, preferred):
		return "", usermessage.New("所选模型已不可用，请重新选择")
	case preferred != "":
		return preferred, nil
	case len(g.Models) == 1:
		return g.Models[0], nil
	default:
		return "", usermessage.New("请选择 Magpie 模型")
	}
}

func magpieBaseURL(port int) string {
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

func magpieSettingsPort() int {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0
		}
		dir = filepath.Join(home, ".config")
	}
	file, err := os.Open(filepath.Join(dir, "magpie", "settings.json"))
	if err != nil {
		return 0
	}
	defer file.Close()
	var settings struct {
		Port int
	}
	if err := json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&settings); err != nil ||
		settings.Port < 1 || settings.Port > 65535 {
		return 0
	}
	return settings.Port
}

// DiscoverMagpie verifies the gateway before allowing model or translation requests.
func DiscoverMagpie(ctx context.Context, port int) (MagpieGateway, error) {
	ports := []int{port}
	if port == 0 {
		ports = []int{magpieDefaultPort}
		if saved := magpieSettingsPort(); saved != 0 && saved != magpieDefaultPort {
			ports = append([]int{saved}, ports...)
		}
	}
	for _, candidate := range ports {
		base := magpieBaseURL(candidate)
		var hello struct {
			Name string
		}
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := magpieGET(probeCtx, base+"/api/hello", &hello)
		cancel()
		if ctx.Err() != nil {
			return MagpieGateway{}, ctx.Err()
		}
		if err != nil || hello.Name != "magpie" {
			continue
		}
		var catalog struct {
			Data []struct {
				ID              string
				ReasoningLevels []string `json:"reasoning_levels"`
			}
		}
		catalogCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = magpieGET(catalogCtx, base+"/v1/models", &catalog)
		cancel()
		if ctx.Err() != nil {
			return MagpieGateway{}, ctx.Err()
		}
		if err != nil {
			return MagpieGateway{Port: candidate}, usermessage.Explain(err, "无法读取 Magpie 模型列表，请重试")
		}
		gateway := MagpieGateway{Port: candidate, supportsNoReasoning: make(map[string]bool, len(catalog.Data))}
		for _, model := range catalog.Data {
			if _, seen := gateway.supportsNoReasoning[model.ID]; !seen && strings.TrimSpace(model.ID) != "" {
				gateway.supportsNoReasoning[model.ID] = slices.Contains(model.ReasoningLevels, "none")
				gateway.Models = append(gateway.Models, model.ID)
			}
		}
		return gateway, nil
	}
	if port != 0 {
		return MagpieGateway{}, usermessage.New("指定端口未发现 Magpie，请检查端口和运行状态")
	}
	return MagpieGateway{}, usermessage.New("请先启动 Magpie")
}

func magpieGET(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create Magpie request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+magpieClientID)
	resp, err := magpieHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("send Magpie request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Magpie returned HTTP %d", resp.StatusCode)
	}
	const limit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("read Magpie response: %w", err)
	}
	if len(body) > limit {
		return fmt.Errorf("Magpie response exceeds %d bytes", limit)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode Magpie response: %w", err)
	}
	return nil
}

// Magpie resolves the local gateway and model once per translation, including
// across chunks when the source needs more than one chat request.
type Magpie struct {
	endpoint         string
	model            string
	disableReasoning bool
}

func (Magpie) Name() string                             { return "magpie" }
func (Magpie) Available(KeyStore, config.Settings) bool { return true }

func (m Magpie) Connect(ctx context.Context, opts config.MagpieOptions) (Magpie, error) {
	if m.endpoint != "" {
		return m, nil
	}
	gateway, err := DiscoverMagpie(ctx, opts.Port)
	if err != nil {
		return Magpie{}, err
	}
	model, err := gateway.SelectModel(opts.Model)
	if err != nil {
		return Magpie{}, err
	}
	return Magpie{
		endpoint:         magpieBaseURL(gateway.Port) + "/v1/chat/completions",
		model:            model,
		disableReasoning: gateway.supportsNoReasoning[model],
	}, nil
}

func (m Magpie) Translate(ctx context.Context, req Request, _ KeyStore, cfg config.Settings) (Result, error) {
	return m.translate(ctx, req, cfg, false, nil)
}

func (m Magpie) Stream(ctx context.Context, req Request, _ KeyStore, cfg config.Settings, emit func(Result)) (Result, error) {
	return m.translate(ctx, req, cfg, true, emit)
}

func (m Magpie) translate(ctx context.Context, req Request, cfg config.Settings, stream bool, emit func(Result)) (Result, error) {
	target := englishName(req.TargetLang)
	if target == "" {
		return Result{}, unsupported("Magpie", req.TargetLang)
	}
	m, err := m.Connect(ctx, cfg.Services.Magpie)
	if err != nil {
		return Result{}, err
	}
	payload := map[string]any{
		"model":    m.model,
		"messages": translationMessages(req, target),
	}
	if m.disableReasoning {
		payload["reasoning_effort"] = "none"
	}
	choice, err := requestChatCompletion(ctx, magpieHTTPClient, m.endpoint, magpieClientID, payload, "Magpie", stream, func(text string) {
		if emit != nil {
			emit(Result{Service: m.Name(), Text: cleanOpenAIText(text), Streaming: true})
		}
	})
	return Result{Service: m.Name(), Text: cleanOpenAIText(choice.Message.Content)}, err
}
