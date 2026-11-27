package app

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/capture"
	"sotto/internal/config"
	"sotto/internal/history"
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

func (a *application) startMinimalJob(task *entryTask, bounds mygo.Rectangle, run func(context.Context, func(string)) translate.Result) {
	ctx, ok := a.beginMinimalJob(task)
	if !ok {
		return
	}
	id := task.id
	source := task.overlaySource(bounds)
	job := a.minimalJobs[id]
	go runMinimalJob(ctx, run, func(text string) {
		mygo.RunOnMain(func() {
			if a.minimalJobs[id] != job || !a.canPresentMinimalJob(job) {
				return
			}
			if job.result == nil {
				job.result = a.overlays.ShowPending(source, text, func() { a.finishMinimalJob(id) })
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
				a.overlays.Show(source, result.Text, result.Err)
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

func (a *application) showMinimalEntryError(task *entryTask, bounds mygo.Rectangle, message string) {
	mygo.RunOnMain(func() {
		if task.Valid() && !a.isQuitting() {
			a.overlays.Show(task.overlaySource(bounds), "", message)
		}
	})
}

func (t *entryTask) overlaySource(bounds mygo.Rectangle) overlay.Source {
	return overlay.Source{EntryID: t.id, Bounds: bounds, Workspace: t.workspace}
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
	record := prepared.history
	if ctx.Err() != nil {
		record.Done()
		return translate.Result{}
	}
	if err != nil {
		return translate.Result{Err: translate.UserMessage(err)}
	}
	if len(prepared.settings.Services.Enabled) == 0 {
		record.Done()
		return translate.Result{Err: "请在设置中启用一个翻译服务。"}
	}
	var final translate.Result
	var partial string
	translate.RunWith(ctx, []translate.Translator{prepared.provider}, prepared.settings, prepared.credentials, prepared.request, func(result translate.Result) {
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
		record.Done()
		return translate.Result{}
	}
	final.Text = strings.TrimSpace(final.Text)
	if final.Text == "" {
		final.Text = strings.TrimSpace(partial)
		if final.Err == "" {
			final.Err = "翻译服务未返回完整译文，请重试。"
		}
	}
	if record != nil {
		// Completing the visual job cancels its context after accepting the result.
		// That must not cancel the independent write of an already complete result.
		go func() {
			defer record.Done()
			a.recordTranslation(context.WithoutCancel(ctx), record, final)
		}()
	}
	return final
}

type minimalRequest struct {
	request     translate.Request
	settings    config.Settings
	credentials keys.Snapshot
	provider    translate.Translator
	history     *history.Request
}

func (a *application) prepareMinimalRequest(ctx context.Context, text string) (_ minimalRequest, err error) {
	cfg := a.settings()
	if err := ctx.Err(); err != nil {
		return minimalRequest{}, err
	}
	request, err := prepareTranslationRequest(text, "", "", cfg)
	if err != nil {
		return minimalRequest{}, err
	}
	request.TranslationOnly = true
	// Snapshot recording before a keyring read can delay the dispatch past a
	// clear or recording toggle. The direction belongs to this original input.
	record := a.beginHistory(text, request, cfg.Services.Order)
	defer func() {
		if err != nil {
			record.Done()
		}
	}()
	service, credentials, err := a.pickMinimalProvider(ctx, &cfg)
	if err != nil {
		return minimalRequest{}, err
	}
	cfg.Services.Enabled = nil
	if service != nil {
		cfg.Services.Enabled = []string{service.Name()}
	}
	return minimalRequest{request: request, settings: cfg, credentials: credentials, provider: service, history: record}, nil
}

func (a *application) pickMinimalProvider(ctx context.Context, cfg *config.Settings) (translate.Translator, keys.Snapshot, error) {
	tried := make(map[string]bool)
	// Rebuild the remaining candidates after a settings write or a failed probe.
	next := func() (translate.Translator, []string) {
		cfg.Services = a.settings().Services
		for _, service := range translate.EnabledProviders(cfg.Services) {
			if !tried[service.Name()] {
				return service, keys.ForServices(service.Name())
			}
		}
		return nil, nil
	}
	var discoveryErr error
	for ctx.Err() == nil {
		service, names := next()
		var credentials keys.Snapshot
		if len(names) != 0 {
			// A writer may have changed the first provider while we waited for the keyring.
			a.configWriteMu.Lock()
			service, names = next()
			if len(names) != 0 && ctx.Err() == nil {
				credentials = a.keys.Snapshot(names)
			}
			a.configWriteMu.Unlock()
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if credentials != nil {
				if err := credentials.Err(names...); err != nil {
					return nil, nil, err
				}
			}
		}
		if service == nil {
			if slices.Contains(cfg.Services.Enabled, "magpie") {
				return nil, nil, discoveryErr
			}
			return nil, nil, nil
		}
		tried[service.Name()] = true
		if !service.Available(credentials, *cfg) {
			continue
		}
		if magpie, ok := service.(translate.Magpie); ok {
			resolved, err := magpie.Connect(ctx, cfg.Services.Magpie)
			if err != nil {
				discoveryErr = err
				continue
			}
			service = resolved
		}
		return service, credentials, nil
	}
	return nil, nil, ctx.Err()
}
