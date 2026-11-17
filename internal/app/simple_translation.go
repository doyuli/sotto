package app

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/capture"
	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/langdetect"
	"sotto/internal/ocr"
	"sotto/internal/platform"
	"sotto/internal/translate"
	"sotto/internal/ui/overlay"
	"sotto/internal/ui/snip"
)

const simpleLoadingDelay = 300 * time.Millisecond

type simpleJob struct {
	cancel    context.CancelFunc
	workspace uint64
	result    *overlay.Result
}

func (a *application) startSimpleJob(task *entryTask, source mygo.Rectangle, run func(context.Context, func(string)) translate.Result) {
	ctx, ok := a.beginSimpleJob(task)
	if !ok {
		return
	}
	id := task.id
	job := a.simpleJobs[id]
	go runSimpleJob(ctx, run, func(text string) {
		mygo.RunOnMain(func() {
			if a.simpleJobs[id] != job || !a.canPresentSimpleJob(job) {
				return
			}
			if job.result == nil {
				job.result = a.overlays.ShowPending(text, source, job.workspace, func() { a.finishSimpleJob(id) })
			} else if text != "" {
				job.result.Progress(text)
			}
		})
	}, func(result translate.Result) {
		mygo.RunOnMain(func() {
			switch {
			case !a.finishSimpleJob(id) || result.Text == "" && result.Err == "":
				if job.result != nil {
					job.result.Close()
				}
			case job.result != nil:
				job.result.Complete(result.Text, result.Err)
			default:
				a.overlays.Show(result.Text, result.Err, source, job.workspace)
			}
		})
	})
}

func runSimpleJob(ctx context.Context, run func(context.Context, func(string)) translate.Result, progress func(string), complete func(translate.Result)) {
	timer := time.AfterFunc(simpleLoadingDelay, func() {
		if ctx.Err() == nil {
			progress("")
		}
	})
	result := run(ctx, func(text string) {
		timer.Stop()
		if ctx.Err() == nil {
			progress(text)
		}
	})
	timer.Stop()
	complete(result)
}

// Acquisition is exclusive. Once it hands off its input on the main thread,
// OCR and translation can outlive that entry without canceling other results.
func (a *application) beginSimpleJob(task *entryTask) (context.Context, bool) {
	if !task.Valid() || !a.settings().SimpleTranslation || a.isQuitting() {
		return nil, false
	}
	if _, exists := a.simpleJobs[task.id]; exists {
		return nil, false
	}
	if a.simpleJobs == nil {
		a.simpleJobs = make(map[int64]*simpleJob)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.simpleJobs[task.id] = &simpleJob{cancel: cancel, workspace: task.workspace}
	return ctx, true
}

func (a *application) finishSimpleJob(id int64) bool {
	job, pending := a.simpleJobs[id]
	if !pending {
		return false
	}
	delete(a.simpleJobs, id)
	job.cancel()
	return a.canPresentSimpleJob(job)
}

func (a *application) canPresentSimpleJob(job *simpleJob) bool {
	return job.workspace == platform.WorkspaceGeneration() && a.settings().SimpleTranslation && !a.isQuitting()
}

func (a *application) stopSimpleJobs() {
	for _, job := range a.simpleJobs {
		job.cancel()
	}
	clear(a.simpleJobs)
	a.overlays.Close()
}

func (a *application) cancelStaleSimpleJobs() {
	workspace := platform.WorkspaceGeneration()
	for id, job := range a.simpleJobs {
		if job.workspace != workspace {
			job.cancel()
			delete(a.simpleJobs, id)
		}
	}
	a.overlays.CloseStale(workspace)
}

func (a *application) showSimpleEntryError(task *entryTask, source mygo.Rectangle, message string) {
	mygo.RunOnMain(func() {
		if task.Valid() && a.settings().SimpleTranslation && !a.isQuitting() {
			a.overlays.Show("", message, source, task.workspace)
		}
	})
}

func (a *application) simpleScreenshot(ctx context.Context, shot *snip.Capture, progress func(string)) translate.Result {
	img, err := shot.Crop(ctx)
	if err != nil {
		log.Printf("capture: %v", err)
		return translate.Result{Err: capture.UserMessage(err)}
	}
	ocrCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	result, err := ocr.Recognize(ocrCtx, img)
	cancel()
	if err != nil {
		log.Printf("ocr: %v", err)
		return translate.Result{Err: ocr.UserMessage(err)}
	}
	if strings.TrimSpace(result.Text) == "" {
		return translate.Result{Err: "未识别到文字，请框选完整文字后重试。"}
	}
	return a.simpleTranslation(ctx, result.Text, progress)
}

func simpleLanguages(text, configured string) (source, target string) {
	switch langdetect.Detect(text) {
	case "zh":
		source, target = "zh-HANS", "en"
	case "latin":
		source, target = "en", "zh-HANS"
	default:
		return "", ""
	}
	if configured == "zh-HANS" || configured == "en" {
		target = configured
	}
	return source, target
}

func (a *application) simpleTranslation(ctx context.Context, text string, progress func(string)) translate.Result {
	if err := translate.ValidateInput(text); err != nil {
		return translate.Result{Err: translate.UserMessage(err)}
	}
	cfg := a.settings()
	original := strings.TrimSpace(text)
	text = strings.TrimSpace(prepareTranslationSource(text, cfg.Identifier))
	if text == "" {
		return translate.Result{Err: "清理后的原文为空，请修改原文或调整命名转换设置。"}
	}
	if err := translate.ValidateInput(text); err != nil {
		return translate.Result{Err: translate.UserMessage(err)}
	}
	source, target := simpleLanguages(text, cfg.TargetLanguage)
	if source == "" {
		return translate.Result{Err: "简约翻译仅支持中文和英文。"}
	}
	if source == target {
		return translate.Result{Text: original}
	}
	cfg, credentials, err := a.simpleService(ctx)
	if ctx.Err() != nil {
		return translate.Result{}
	}
	if err != nil {
		return translate.Result{Err: translate.UserMessage(err)}
	}
	if len(cfg.Services.Enabled) == 0 {
		return translate.Result{Err: "请在设置中启用一个翻译服务。"}
	}
	var final translate.Result
	var partial string
	translate.Run(ctx, cfg, credentials, translate.Request{Text: text, SourceLang: source, TargetLang: target}, func(result translate.Result) {
		if result.Streaming {
			if result.Text != partial && strings.TrimSpace(result.Text) != "" {
				partial = result.Text
				progress(partial)
			}
			return
		}
		final = result
	})
	if ctx.Err() != nil {
		return translate.Result{}
	}
	final.Text = strings.TrimSpace(final.Text)
	if final.Text == "" {
		final.Text = strings.TrimSpace(partial)
		if final.Err == "" {
			final.Err = "翻译服务未返回完整译文，请重试。"
		}
	}
	return final
}

func (a *application) simpleService(ctx context.Context) (config.Settings, keys.Snapshot, error) {
	cfg := a.settings()
	services := translate.EnabledProviders(cfg.Services)
	if len(services) == 0 {
		cfg.Services.Enabled = nil
		return cfg, nil, nil
	}
	if first := services[0]; len(keys.ForServices(first.Name())) == 0 && first.Available(nil, cfg) {
		cfg.Services.Enabled = []string{first.Name()}
		return cfg, nil, nil
	}
	// Read provider options and credentials in the same settings transaction.
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()
	cfg = a.settings()
	for _, service := range translate.EnabledProviders(cfg.Services) {
		if err := ctx.Err(); err != nil {
			return cfg, nil, err
		}
		var credentials keys.Snapshot
		if names := keys.ForServices(service.Name()); len(names) != 0 {
			credentials = a.keys.Snapshot(names)
			if err := credentials.Err(names...); err != nil {
				return cfg, nil, err
			}
		}
		// A provider with missing credentials is not enabled in Settings.
		if service.Available(credentials, cfg) {
			cfg.Services.Enabled = []string{service.Name()}
			return cfg, credentials, nil
		}
	}
	cfg.Services.Enabled = nil
	return cfg, nil, nil
}
