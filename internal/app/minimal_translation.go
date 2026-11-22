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
	"sotto/internal/platform"
	"sotto/internal/translate"
	"sotto/internal/ui/overlay"
	"sotto/internal/ui/snip"
)

const minimalLoadingDelay = 300 * time.Millisecond

type minimalJob struct {
	cancel    context.CancelFunc
	workspace uint64
	result    *overlay.Result
}

func (a *application) startMinimalJob(task *entryTask, source mygo.Rectangle, run func(context.Context, func(string)) translate.Result) {
	ctx, ok := a.beginMinimalJob(task)
	if !ok {
		return
	}
	id := task.id
	job := a.minimalJobs[id]
	go runMinimalJob(ctx, run, func(text string) {
		mygo.RunOnMain(func() {
			if a.minimalJobs[id] != job || !a.canPresentMinimalJob(job) {
				return
			}
			if job.result == nil {
				job.result = a.overlays.ShowPending(text, source, job.workspace, func() { a.finishMinimalJob(id) })
			} else if text != "" {
				job.result.Progress(text)
			}
		})
	}, func(result translate.Result) {
		mygo.RunOnMain(func() {
			switch {
			case !a.finishMinimalJob(id) || result.Text == "" && result.Err == "":
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

func runMinimalJob(ctx context.Context, run func(context.Context, func(string)) translate.Result, progress func(string), complete func(translate.Result)) {
	timer := time.AfterFunc(minimalLoadingDelay, func() {
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
func (a *application) beginMinimalJob(task *entryTask) (context.Context, bool) {
	if !task.Valid() || !task.minimal || a.isQuitting() {
		return nil, false
	}
	if _, exists := a.minimalJobs[task.id]; exists {
		return nil, false
	}
	if a.minimalJobs == nil {
		a.minimalJobs = make(map[int64]*minimalJob)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.minimalJobs[task.id] = &minimalJob{cancel: cancel, workspace: task.workspace}
	return ctx, true
}

func (a *application) finishMinimalJob(id int64) bool {
	job, pending := a.minimalJobs[id]
	if !pending {
		return false
	}
	delete(a.minimalJobs, id)
	job.cancel()
	return a.canPresentMinimalJob(job)
}

func (a *application) canPresentMinimalJob(job *minimalJob) bool {
	return job.workspace == platform.WorkspaceGeneration() && !a.isQuitting()
}

func (a *application) stopMinimalJobs() {
	for _, job := range a.minimalJobs {
		job.cancel()
	}
	clear(a.minimalJobs)
	a.overlays.Close()
}

func (a *application) cancelStaleMinimalJobs() {
	workspace := platform.WorkspaceGeneration()
	for id, job := range a.minimalJobs {
		if job.workspace != workspace {
			job.cancel()
			delete(a.minimalJobs, id)
		}
	}
	a.overlays.CloseStale(workspace)
}

func (a *application) showMinimalEntryError(task *entryTask, source mygo.Rectangle, message string) {
	mygo.RunOnMain(func() {
		if task.Valid() && !a.isQuitting() {
			a.overlays.Show("", message, source, task.workspace)
		}
	})
}

func (a *application) minimalScreenshot(ctx context.Context, shot *snip.Capture, progress func(string)) translate.Result {
	img, err := shot.Crop(ctx)
	if err != nil {
		log.Printf("capture: %v", err)
		return translate.Result{Err: capture.UserMessage(err)}
	}
	text, message := recognizeScreenshot(ctx, img)
	if message != "" {
		return translate.Result{Err: message}
	}
	return a.minimalTranslation(ctx, text, progress)
}

func (a *application) minimalTranslation(ctx context.Context, text string, progress func(string)) translate.Result {
	prepared, err := a.prepareMinimalRequest(ctx, text)
	if ctx.Err() != nil {
		return translate.Result{}
	}
	if err != nil {
		return translate.Result{Err: translate.UserMessage(err)}
	}
	if len(prepared.settings.Services.Enabled) == 0 {
		return translate.Result{Err: "请在设置中启用一个翻译服务。"}
	}
	var final translate.Result
	var partial string
	translate.Run(ctx, prepared.settings, prepared.credentials, prepared.request, func(result translate.Result) {
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

type minimalRequest struct {
	request     translate.Request
	settings    config.Settings
	credentials keys.Snapshot
}

func (a *application) prepareMinimalRequest(ctx context.Context, text string) (minimalRequest, error) {
	cfg := a.settings()
	services := translate.EnabledProviders(cfg.Services)
	if len(services) > 0 {
		first := services[0]
		if len(keys.ForServices(first.Name())) != 0 || !first.Available(nil, cfg) {
			// Read provider options and credentials in the same settings transaction.
			a.configWriteMu.Lock()
			defer a.configWriteMu.Unlock()
			cfg = a.settings()
			services = translate.EnabledProviders(cfg.Services)
		}
	}
	if err := ctx.Err(); err != nil {
		return minimalRequest{}, err
	}
	request, err := prepareTranslationRequest(text, "", "", cfg)
	if err != nil {
		return minimalRequest{}, err
	}
	request.TranslationOnly = true
	for _, service := range services {
		if err := ctx.Err(); err != nil {
			return minimalRequest{}, err
		}
		var credentials keys.Snapshot
		if names := keys.ForServices(service.Name()); len(names) != 0 {
			credentials = a.keys.Snapshot(names)
			if err := credentials.Err(names...); err != nil {
				return minimalRequest{}, err
			}
		}
		// A provider with missing credentials is not enabled in Settings.
		if service.Available(credentials, cfg) {
			cfg.Services.Enabled = []string{service.Name()}
			return minimalRequest{request: request, settings: cfg, credentials: credentials}, nil
		}
	}
	cfg.Services.Enabled = nil
	return minimalRequest{request: request, settings: cfg}, nil
}
