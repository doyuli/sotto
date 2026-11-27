# Translation Window State and Lifecycle

This document records the ownership and lifecycle constraints of translation requests and native views. See [verification entry points](../e2e/README.md) for Tester coverage and desktop acceptance on Windows and macOS.

## State Ownership

| Scope | Fields | Meaning and Update Conditions |
|---|---|---|
| Global entry task | `entryTask.id / ctx` | A new input, selection, or OCR entry invalidates previous selection/recognition work. Switching entries also dismisses the unpinned bubble and cancels its work. |
| Window object | `bubble.id`, `win` | Native window identity, unchanged throughout its lifetime. Input, selection, and OCR share one reusable window; pinning removes it from reuse. |
| Content session | `sessionID` | Identity of a session for one set of source content. A new input, selection, or OCR entry replaces the session. Reopening an already-visible, unpinned input panel only focuses it and preserves the session if the current input entry is still valid. |
| Snapshot order | `generation` | Increasing content-session sequence within a window, used by the native view to reject old session snapshots. A new sessionID is an opaque string and cannot be compared chronologically. |
| Presentation | `showID` | Increments on every explicit invocation, even if sessionID and source text are unchanged. Prevents Close from a previous presentation from closing a reopened panel. |
| Presentation | `closeClaimed` | Close is accepted at most once for the same showID; reset on a new presentation. Native close also sets it. |
| Request order | `revision` | Highest request/cancel/close version accepted by the backend; incremented by the native view before sending a new command. Orders full-text requests, cancellation, and closure, and synchronizes snapshots. |
| Full-text boundary | `fullRevision` | Boundary of the latest full-text request or cancel/close. A single-card retry must be newer than this boundary, but need not be newer than another service's retry. |
| Current request | `reqID` | Request corresponding to the highest revision, used for the view's overview and latest metadata. Must not reject still-valid results from other services. Cleared on cancel/close. |
| Full-text request | `fullReqID` | Identity of the current full-text request. Late metadata may still register services that have not been retried, but must not overwrite retried services. |
| Service order | `serviceRevisions[service]` | Latest accepted retry version per service; prevents older retries from overwriting the same card out of order. Reset on a full-text request. |
| Backend service identity | `serviceRequests[service]` | The reqID whose worker results may be queued for this service. Registered atomically when a retry starts, without waiting for credential reads. |
| Running work | `runCancels[reqID]` | Cancellation function per request. A full-text request may still have other unfinished services, so a single-card retry does not cancel it. Removed on completion; all are canceled on full-text replacement/cancel/close. |
| Recognition phase | `cancelCapture`, `phase` | Cancellation function and presentation phase before OCR becomes a translation request. Recognition results may update only the original session's window while revision remains -1. |
| Deferred display | `canShow`, `ready`, `presentationShown` | First display occurs after the native view applies the current state, the window is ready, and the entry remains valid. No late popup is allowed after close or entry invalidation. |
| Workspace presentation | `workspace` | Workspace generation recorded on presentation or explicit restoration. Independent of `canShow` entry ownership; restoring a pinned bubble must not reclaim its old entry. |

Translation metadata and result content belong to the native view on the UI thread. `bubbleView.cards` is the sole retained result store; `runServices`, language fields, and `serviceReqIDs` describe the displayed requests. The backend retains request identities and cancellation handles, not a second metadata/result cache. Stable control identities preserve editing, selection, and focus state.

`BubbleState` carries source content, settings, and presentation identity for a new session or OCR completion before translation starts. It does not carry translation cards or service-request maps and is not used to reconstruct ongoing results. Refocusing an existing visible panel uses `onPresented`; dismissal and window closure use `onStopped` to settle the existing cards while preserving their text. Clearing the composer cancels work and removes its cards immediately.

`PushMeta(win, meta, retry)` registers backend service ownership and queues metadata through `Window.Update`; `PushResult(win, result)` validates worker ownership and queues the result. The backend guard rejects obsolete workers before dispatch, while the view's `serviceReqIDs` guard rejects callbacks overtaken by a retry, dismissal, or replacement while waiting in the UI queue. These checks protect different sides of the asynchronous boundary; removing the duplicate result cache does not remove either identity guard.

When blur, an outside click, or a new entry dismisses an unpinned bubble, the current presentation session ends: cancel full-text requests, single-card retries, and OCR; clear result identities; set `closeClaimed`; and emit `RunStopped`. Reopening after hiding starts a new session and clears the input draft, translations, and request identities. If the current input entry remains valid and the unpinned panel is already visible, reopening only focuses it: it does not revoke the entry, reset content, or stop in-flight translation. It only increments showID to invalidate late close, blur, and outside-click callbacks from the old presentation. Pinned windows retain independent sessions.

## Presentation and Closure

### Opening and Closing Each Window

| Window | Opening and Reuse | Hiding, Closing, and Quitting |
|---|---|---|
| Input panel | If the current entry is valid and the panel is unpinned and visible, reopening only focuses it and advances showID, preserving the draft, languages, and work. Other input entries hide screenshot overlays, end the old presentation, and start a blank session in the shared window. | With auto-hide enabled, blur or an outside click ends the current presentation session: cancel work, clear request identities, emit `RunStopped`, then hide. Esc/the close button also cancels work before hiding. Reopening after hiding replaces the session. |
| Selection/OCR bubble | Shares the input panel's window; each new entry replaces the session. Selection takes over the window only after capture completes and remains valid. OCR first shows recognition state, then updates the recognition result within the same session. | Blur or an outside click with auto-hide enabled, as well as entry switches or closure, ends the current presentation session: cancel capture/translation, clear request identities, and emit `RunStopped`. Later results must not update or redisplay the hidden window. |
| Screenshot overlay | The OCR entry hides the transient bubble, captures display snapshots, and creates a native overlay for each usable snapshot. | Esc, right-click, native close, or focus leaving the overlay group cancels the round. Input and selection entries also revoke it. Submission and cancellation clear the preview state, hide the windows, and queue their destruction on the UI thread. |
| Pinned bubble | Leaves the shared reuse slot; subsequent entries create a window as needed. Unpinning returns it to the shared slot, canceling and destroying the displaced transient window even if that window is already hidden. | Does not automatically hide on blur. Closing actually destroys the window and stops its work. |
| Settings window | Independent singleton; reopening calls Show/Focus without acquiring translation entry ownership. | Closing destroys the native window. The app retains the settings controller and the same task queue; reopening inherits pending save state. Opening settings may trigger bubble auto-hide through blur. |
| Application quit | OnBeforeQuit sets quitting and revokes the entry; new entries are no longer dispatched. | All window OnClose handlers allow closure, preserving separate paths for tray residency and actual quitting. |

```mermaid
stateDiagram-v2
    [*] --> Hidden
    Hidden --> WaitingReady: ShowTask / new sessionID, showID++
    Presented --> Presented: Reopen current visible input / preserve state and focus
    Presented --> WaitingReady: New content entry / cancel old session and create a new one
    WaitingReady --> Presented: Native state applied + window ready + entry valid
    WaitingReady --> Hidden: Close or entry invalidation
    Presented --> Hidden: Blur/outside click/entry dismissal / cancel work, clear identities, emit RunStopped
    Presented --> Hidden: Valid Close / cancel work and advance revision
    Presented --> Presented: Close with stale showID or revision / ignore
    Presented --> Pinned: Pin / leave reuse slot
    Pinned --> Presented: Unpin / restore ordinary window behavior
    Pinned --> Closed: Valid Close
    Hidden --> Closed: Quit
    Presented --> Closed: Quit
    Pinned --> Closed: Quit
```

1. `ShowTask` is entered from a worker goroutine. Selecting or creating the shared window, committing presentation, and validation/native operations for `SetPinned` and `Close` run on the main thread.
2. Close must match the window, sessionID, and showID, carry a newer revision, and find closeClaimed unset. The native view reserves an incremented revision before calling, but does not clear active requests or cards early; rejection or failure does not roll back the consumed sequence number. The backend validates before canceling and closing. Do not split cancellation into a separate call unprotected by showID.
3. `OnClose` advances revision, cancels work, and emits RunStopped with showID. An accepted Close ends the current presentation; the next command for that presentation, issued while closure is pending, may be rejected by this boundary. The view settles its state using the matching RunStopped. If reopening advances showID on the UI thread first, the old Close is rejected, and the old RunStopped must not affect the new presentation. Ordinary windows prevent destruction and hide; pinned windows allow destruction. Blur, outside clicks, and dismissal by a new entry end unpinned presentations with the same semantics: immediately cancel work, clear request identities, and emit RunStopped.
4. After `OnBeforeQuit` sets quitting, every OnClose must allow closure to avoid interrupting the application's quit sequence.
5. `BubblePresented` only synchronizes presentation identity, the version after closure, and restoration state; it does not reset drafts. Late events with an old showID must not affect a new presentation.
6. `OnBlur` defers its check to the next main-thread dispatch. Hide only a visible window with the same showID, auto-hide still enabled, and focus actually lost. Outside-click callbacks also validate sessionID and showID; old callbacks must not hide a new presentation.
7. `SetPinned` validates the calling window, sessionID, and showID, rejecting rounds already closed or not yet presented. The view updates the current session's pinned state only after acceptance.

The `Shown` returned by `ShowTask` means the presentation was accepted, not that the native window is already visible. It returns the zero value when the entry is invalid or the application's UI loop has stopped. Callers that start subsequent work must first check `Shown.Win != nil`. The current input entry does not read the return value; selection and OCR callers both check the window. Normal cancellation should not be treated as an error requiring a popup.

## Requests and Cancellation

```mermaid
stateDiagram-v2
    [*] --> Ready
    Ready --> Running: Submit full text / new revision, register full-text boundary
    Running --> Running: Submit new full text / cancel old work, reset card identities
    Running --> Running: Retry one card / update only that service's identity
    Running --> Ready: All requests complete / retain results
    Running --> Ready: Cancel or Close / advance boundary, reject stale results
    Ready --> Running: Retry a failed card
```

Key invariants:

- Requests with a mismatched sessionID are invalid; revisions cannot be compared across sessions.
- A dismissed or closed unpinned presentation rejects new full-text requests, single-card retries, OCR completion, and late first display. A new `replaceContent` resets this boundary and permits new work.
- New input, selection, and OCR entries immediately call `HideTransient` before presentation/capture begins, ending the old unpinned bubble's session without waiting for new content. Selection first passes the configured shortcut to `selection.Prepare` to preserve the source, then dismisses the old bubble. `replaceContent` again ensures all old requests, including retries, are canceled; new requests start only after session and result identities are replaced. Failed selection capture does not restore the old session. Old HTTP completions or late metadata must not overwrite the new source text.
- `closeClaimed` blocks subsequent full-text requests, single-card retries, OCR writeback, and late first display for a dismissed session, even when a late command carries a higher revision. Only a new session resets this boundary; dismissing a transient bubble does not affect pinned windows.
- Full-text requests must be newer than the global revision. A single-card retry need only exceed both fullRevision and that service's retry version.
- Retries for different services may arrive out of order. Accepting an earlier independent retry must not lower the global revision.
- Accept results only when `serviceRequests[service] == reqID`; a simple comparison against the global reqID is insufficient.
- Full-text metadata must not reclaim a card replaced by a retry. Old request metadata must not be registered after cancellation.
- OCR completion does not reopen the window; it only updates the session that still belongs to that recognition task. Old OCR must not publish after the user clears, edits and submits, or cancels.

## Streaming Translations

- Bilibili, OpenAI (including compatible endpoints), SiliconFlow, and Zhipu request Chat Completions SSE. Read only `delta.content` from the first choice, ignoring role, reasoning, and usage. A full translation is still accepted if a compatible endpoint returns ordinary JSON.
- SSE has no size limit on the entire transport body, so repeated metadata and ignored reasoning do not consume the translation budget. The provider parser limits individual events (including multiline data) and accumulated translated text within one response to 1 MiB each; ordinary JSON responses also have a 1 MiB transport limit. Above that parser, translation orchestration enforces the stricter 256 KiB cumulative output budget per service across all source chunks. Exceeding either layer's limit preserves accepted text and returns an explicit error. Connection and TLS handshake timeouts are 10 seconds each; response headers have 30 seconds. Allow up to 60 seconds for the first body bytes; thereafter cancel only after 30 consecutive seconds without new bytes. Reasoning and heartbeats also reset the idle timer. Continuous output has no overall duration limit. A timeout preserves partial text and reports "生成超时，译文未完成，请重试".
- `TranslationUpdateMsg.text` always contains cumulative translation text; `streaming=true` means generation is ongoing. The final update has `streaming=false`; on failure it includes both `err` and the text already received. Chunked long text retains completed sections and partial output from the current section.
- Publish the first nonempty text immediately, then update the native view at most 25 times per second. Always publish a terminal state on completion or failure. Native cards retain only the latest cumulative result per service. Cancellation settles loading and streaming cards as errors in place, retaining partial text and clearing "生成中" without replaying a backend snapshot.
- Skeletons last only until the first text arrives. Show "生成中" during generation; enable copy, speech, and naming formats only after successful completion. Disconnection, truncation, and cancellation retain text with an incomplete notice and a retry action. Incremental updates reuse the same card state and control identities, preserving reading position and keyboard focus.
- Settings connection tests use ordinary translation requests with `stream=false`, retain an independent 12-second deadline, and show "连接成功" on success. Normal translation uses `stream=true`, does not automatically resend because the first bytes arrive slowly, and does not treat EOF without a completion marker as success.
- Shared Chat requests first use provider business codes to distinguish model, balance, quota, permission, and input-length errors, then fall back to HTTP status. Read at most 16 KiB from failed HTTP responses; do not display the raw body. JSON and SSE errors share parsing, and streaming failures preserve existing text.
- All incremental and terminal updates still pass through the existing `serviceRequests` / `serviceReqIDs` filters. Old streams cannot reclaim cards after retries, full-text replacement, or cancellation.

## Service Configuration

- OpenAI, SiliconFlow, and Zhipu save model settings independently; leaving a model blank restores its default. Credentials remain in the system keyring.
- All three share model-draft logic. Settings synchronization preserves unsubmitted input. Changing a model clears the old test result; model fields and the OpenAI address are locked during testing.
- Connection tests start after preceding settings tasks in the queue finish. If the latest save failed, abort the test; retry is allowed after a later successful save. Show save errors and read back from the backend after the queue drains. A failure does not stop the save queue. Closing settings still drains submitted snapshots and saves model/address drafts.

## Locks and Native View Closure

DeepL quota queries run only for an expanded service card. Repeated expansion reuses an in-flight query or its successful result for the current window; failed queries can retry on the next expansion. Saving or clearing the credential cancels the old query and invalidates its result before the keyring write starts. Closing settings also cancels and invalidates quota work; reopening fetches fresh usage. Generation checks still reject late results even if cancellation arrives after completion. Finishing a credential write after window closure must not restart a quota query.

`m.mu` protects window state. While holding it, do not call native window methods that may wait for the main thread. Main-thread callbacks dispatch entry work; window reuse, creation, and destruction are committed serially on the main thread.

Bubble view state belongs to the UI thread. Publish background results through `Window.Update`, validating session and request identities before applying them. Closing or hiding an unpinned bubble cancels work according to its presentation lifecycle; pinned windows are independent. The app owns the settings controller, whose state is mutex-protected. Settings saves, credential writes, and state refreshes share a serial task queue. Closing the window flushes model/address drafts and clears temporary UI state; reopening reuses optimistic settings. Connection tests and quota queries are canceled; generation checks reject late results. `Dispose` is reserved for final exit or test cleanup: it stops accepting tasks and drains queued work. The quit listener defers quitting while tasks remain, waits for `taskDone` in the background, then calls Quit again. The UI thread must continue processing tasks' native calls; it must not wait synchronously for the queue.

## Clipboard Restoration

Sotto restores the previous clipboard when it has a complete backup and can verify that the simulated copy is still current. If it detects a newer copy or cannot verify ownership, it leaves the clipboard alone.

Once Sotto has read a stable selection, it can translate it even if restoration fails. Discard the result if the source window changes or user input cancels capture before delivery. A later shortcut or manual translation can proceed normally.

Platform-specific checks are documented in the [Windows clipboard fallback](windows.md#3-selection-text-capture-and-clipboard-discipline) and [macOS selection capture](macos.md#3-selection-text-capture) sections.

## State Verification Boundaries

- **Session and presentation state**: `internal/ui/bubble/session_test.go` and `dismiss_test.go` verify request ordering, service isolation, workspace transitions, rejection of stale closes, and settling native cards through `onStopped`. Native UI tests (`ui_test.go`, `controls_test.go`, `scroll_test.go`) exercise streaming, retry, and sizing.
- **Clipboard & pasteboard protection**: `internal/selection` unit tests cover Windows DIB/palette snapshots, format write fault injection, and macOS isolated item snapshots with version guards.
- **Cross-document boundaries**: Hotkey registration transactions are specified in [hotkeys.md](hotkeys.md); platform architectures in [windows.md](windows.md) and [macos.md](macos.md).
- **Desktop acceptance**: Automated tests and Tester do not replace physical hardware validation. macOS manual desktop acceptance has been confirmed by the maintainer (see [macOS acceptance](../e2e/macos.md)); Windows manual desktop acceptance has also been confirmed by the maintainer; its regression procedures are documented in [Windows acceptance](../e2e/windows.md).

## New Sessions and Content Size Updates

- Before `entry.begin()`, the input entry checks whether the currently visible input still owns the entry. If so, it preserves the presented state, increments showID, and focuses through the native view's `onPresented`, retaining the draft and in-flight work. `Close` accepts only the current sessionID, showID, and a newer revision; stale closes cannot clear active requests. Other entries use `ShowTask` to run `replaceContent`, cancel old session work, increment generation, and create a new sessionID. Hide the reused window and reset its size and position before applying the new state to the native view.
- Bubbles use `ui.View`. The native view measures content height on the UI thread and passes it to the window through `onHeightMeasured`. Height and work-area limits use native logical units; `bubbleLayoutHeight(height, limit)` handles clamping and fractional rounding. Use native measurement and window-size APIs directly.
- Fit the first visible translation at the opening anchor. Subsequent measurements keep the window's top edge fixed and cap the viewport at the available work area below it. The body scrolls when the measured content exceeds that viewport. Automatic updates apply the target size immediately. Explicit card toggles ease out over 100 ms when expanding and 150 ms when collapsing. The toggle request remains pending until the next committed layout measurement, including when another service updates in the same frame. Hidden windows and Reduce Motion apply the target size immediately. `SetBounds` commits each size and position together. Closed presentations reject measurements, and measurement itself never triggers first display. Later size updates for pinned windows are unaffected when a new entry invalidates the old canShow.
- `ShowTask` attempts display after the native view applies the current snapshot. If the window is not ready, `OnReadyToShow` retries and rechecks entry validity. Under the lock, `claimShow` allows each valid, unclosed presentation to display only once. Reused windows retain native ready state.
- OCR completion updates only an unclosed presentation still belonging to that sessionID with revision still -1. It does not replace generation or showID, or reopen the window. Asynchronous snapshots return to the UI thread through `Window.Update`; the view's session/request identity checks reject stale work.
- MyGo builds views on the main thread (`ui/doc.go`). `Window.Update` queues the state mutation and a redraw on that thread (`content.go`). `SetContentSize` takes the content area's dimensions in window DIPs (`window.go`).

## macOS Application and Workspace Events

Passive results do not receive focus, so blur alone cannot dismiss them. Outside clicks and NSWorkspace application-activation notifications jointly detect departure. Listeners belong to the current presentation; the target app must still be in the foreground, and old listeners or showIDs must not close a new presentation. Ordinary app switches do not advance workspace generation; pinned bubbles and disabled auto-hide retain their existing semantics.

Space/sleep changes advance workspace generation. Stale transient presentations must be closed semantically: cancel full-text requests, single-card retries, and recognition; clear result identities; and settle view state. Calling Hide alone is insufficient. Pinned bubbles only hide and retain independent requests. Menu actions explicitly restore numbered windows that are still alive and pinned. Restoration updates only the `workspace` presentation boundary and preserves the original `canShow` entry check. When unpinning after restoration and reopening the input panel, an invalid old entry requires acquiring a new entry and revoking other entries still capturing content.

Shared state machines and injected operations are not acceptance tests of real NSWorkspace, AX, SCK, or pasteboard behavior. See `../e2e/README.md` for platform tests and desktop verification entry points.


## Translation resource budgets

A complete translation accepts at most 18,000 Unicode characters,
with at most 256 KiB of cumulative translated UTF-8 text per service. The existing
1,800-character provider chunks and transport limits still apply. Oversized input
is rejected explicitly; oversized generated output cancels that service's request
and keeps its last accepted partial text. Source selections are checked before
creating a result view, and input-panel submissions preserve the draft on rejection.
All services share a single split of the source. Streaming updates retain a prefix
and current chunk separately, concatenating only when the 25 Hz publisher emits.
