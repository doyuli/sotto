<script module lang="ts">
  // Route remounts share the page's counter, including IPC still in flight.
  let layoutSequence = 0;
</script>

<script lang="ts">
  import { onMount, tick } from "svelte";
  import { SvelteMap } from "svelte/reactivity";
  import {
    BubbleSvc,
    SnipSvc,
    TranslateSvc,
    events,
    type BubbleState,
    type RunMetaMsg,
    type Settings,
    type TranslationUpdateMsg,
  } from "../mygo";
  import { detectedName, langName, speechTag, LANGUAGES, SERVICE_NAMES } from "../languages";
  import { scrollActivity } from "../utils/scroll-activity";
  import { requestID } from "../utils/request-id";
  import { createToasts } from "../utils/toasts.svelte";
  import BubbleIcon from "./BubbleIcon.svelte";
  import TranslationCard, { type CardState } from "./TranslationCard.svelte";
  import ToastStack from "./ToastStack.svelte";

  const { messages, show: toast } = createToasts();

  let { id: propID }: { id?: number } = $props();

  let bubbleID = $state(0);
  let bubbleState = $state<BubbleState | null>(null);
  let reqID: string = $state("");
  let runServices: Set<string> | null = $state(null);
  let sourceLang: string = $state("auto");
  let targetLang: string = $state("zh-HANS");
  let detected: string = $state("");
  let revision: number = $state(0);
  let submittedText: string = $state("");
  let retryService: string = $state("");
  let readyFailed = $state(false);
  let pendingRecovery = $state("");

  let inputText: string = $state("");
  let textareaElem: HTMLTextAreaElement | undefined = $state();
  let bubbleElem: HTMLDivElement | undefined = $state();
  let scrollElem: HTMLDivElement | undefined = $state();
  let maxHeight = $state(760);

  let lastLayout: { key: string; retries: number } | undefined;
  let layoutTimer: ReturnType<typeof setTimeout> | undefined;

  function cancelLayout(): void {
    clearTimeout(layoutTimer);
    layoutTimer = undefined;
    lastLayout = undefined;
  }

  const cards = new SvelteMap<string, CardState>();
  const cardRequests = new Map<string, string>();
  let collapsedCards: Record<string, boolean> = $state({});

  const enabledServices = $derived(
    bubbleState ? activeServices(bubbleState.settings).filter((name) => !runServices || runServices.has(name)) : []
  );
  const showingServices = $derived(enabledServices.filter((name) => cards.has(name)));

  let isRecognizing: boolean = $derived(bubbleState?.phase === "recognizing");
  let pageVisible = $state(!document.hidden);
  let isPinned: boolean = $derived(bubbleState?.pinned ?? false);

  let statusMessage: string = $derived.by(() => {
    if (isRecognizing) return "正在识别文字…";
    const dirty = Boolean(submittedText && inputText.trim() !== submittedText.trim());
    if (dirty) return "原文已修改，按 Enter 重新翻译";
    if (bubbleState?.message) return bubbleState.message;
    if (bubbleState?.trigger === "ocr") return "识别文字可直接修改，按 Enter 重新翻译";
    return "";
  });

  let detectedDisplay: string = $derived.by(() => {
    const src = detected || sourceLang;
    if (src && src !== "auto" && src !== "latin" && src !== "unknown") {
      return detectedName(src);
    }
    return "";
  });

  function activeServices(s: Settings): string[] {
    return s.services.order.filter((name) => s.services.enabled.includes(name));
  }

  function growTextarea(): void {
    if (!textareaElem) return;
    textareaElem.style.height = "auto";
    textareaElem.style.height = `${textareaElem.scrollHeight}px`;
  }

  function apply(st: BubbleState): void {
    if (
      bubbleState &&
      (st.generation < bubbleState.generation ||
        (st.sessionID === bubbleState.sessionID && (st.revision < bubbleState.revision || st.showID < bubbleState.showID || st.layoutID < bubbleState.layoutID)))
    ) {
      return;
    }
    if (st.sessionID !== bubbleState?.sessionID || st.showID !== bubbleState?.showID || st.layoutID !== bubbleState?.layoutID) {
      cancelLayout();
    }
    layoutSequence = Math.max(layoutSequence, st.layoutSequence);
    bubbleState = st;
    maxHeight = st.maxHeight || 760;
    const appliedState = bubbleState;
    readyFailed = false;
    reqID = st.reqID;
    runServices = null;
    detected = "";
    revision = Math.max(0, st.revision);
    submittedText = st.text;
    retryService = "";
    sourceLang = st.sourceLang || "auto";
    targetLang = st.targetLang || "zh-HANS";
    inputText = st.text;

    cards.clear();
    cardRequests.clear();
    for (const [service, id] of Object.entries(st.serviceRequests ?? {})) {
      cardRequests.set(service, id);
    }

    if (st.reqID && st.text) {
      for (const name of activeServices(st.settings)) {
        cards.set(name, { status: "loading" });
      }
    }
    if (st.meta && st.meta.reqID === st.reqID) {
      sourceLang = st.meta.sourceLang;
      targetLang = st.meta.targetLang;
      runServices = new Set(st.meta.services ?? []);
      for (const name of cards.keys()) {
        if (!runServices.has(name)) cards.delete(name);
      }
    }
    for (const msg of st.results ?? []) {
      applyResult(msg);
      if (msg.detectedLang && !detected) detected = msg.detectedLang;
    }

    void tick().then(() => {
      if (bubbleState !== appliedState) return;
      growTextarea();
      if (scrollElem) scrollElem.scrollTop = 0;
      if (st.trigger === "input") textareaElem?.focus();
      reportLayout();
    });
  }

  function observeLayout(node: HTMLDivElement): { destroy: () => void } {
    // Keep the observer with its DOM node if a failed Ready replaces the root.
    const observer = typeof ResizeObserver === "undefined" ? undefined : new ResizeObserver(reportLayout);
    observer?.observe(node);
    return { destroy: () => observer?.disconnect() };
  }

  function reportLayout(): void {
    measureLayout();
  }

  function measureLayout(retry?: NonNullable<typeof lastLayout>): void {
    if (!bubbleState || !bubbleElem || (retry && retry !== lastLayout)) return;
    // Retries also read live geometry; an observer may not have delivered the new size yet.
    const { width, height } = bubbleElem.getBoundingClientRect();
    if (!(height > 0) || !Number.isFinite(height)) {
      cancelLayout();
      return;
    }
    const { sessionID, showID, layoutID } = bubbleState;
    const key = JSON.stringify([sessionID, showID, layoutID, width, height]);
    if (key === lastLayout?.key && !retry) return;
    clearTimeout(layoutTimer);
    const task = lastLayout && key === lastLayout.key ? lastLayout : { key, retries: 0 };
    lastLayout = task;
    let settled = false;
    function finish(accepted: boolean): void {
      // A timeout or a newer measurement owns the timer once this attempt ends.
      if (settled || lastLayout !== task) return;
      settled = true;
      clearTimeout(layoutTimer);
      layoutTimer = undefined;
      if (!accepted && task.retries < 3) {
        layoutTimer = setTimeout(() => {
          if (lastLayout !== task) return;
          task.retries++;
          measureLayout(task);
        }, 50 * (task.retries + 1));
      }
    }
    // Bound both rejected IPC and calls which never settle, without observer-driven loops.
    layoutTimer = setTimeout(() => finish(false), 1000);
    void BubbleSvc.rendered(bubbleID, sessionID, showID, layoutID, ++layoutSequence, height)
      .then((accepted) => finish(accepted === true), () => finish(false));
  }

  function applyResult(msg: TranslationUpdateMsg): void {
    const card: CardState = {
      status: msg.err ? "error" : msg.streaming ? "streaming" : "ok",
      text: msg.text,
      err: msg.err,
    };
    cards.set(msg.service, card);
    if (!msg.err && !msg.streaming && targetLang === "en" && msg.text && msg.service !== "dictionary") {
      void attachFormats(msg.service, card, msg.text);
    }
  }

  async function attachFormats(service: string, card: CardState, text: string): Promise<void> {
    try {
      const formats = await TranslateSvc.formats(text);
      if (cards.get(service) === card) cards.set(service, { ...card, formats });
    } catch {
      // Formats are optional.
    }
  }

  function swapLanguages(): void {
    const from = detected || sourceLang;
    if (!from || from === "auto" || from === "latin" || from === "unknown") {
      toast("先输入文本，检测到源语言后再交换");
      return;
    }
    const fromCode = from === "zh" ? "zh-HANS" : from;
    const prevTarget = targetLang;
    targetLang = fromCode;
    sourceLang = prevTarget;
    detected = "";
    run();
  }

  function cancelCurrent(): void {
    if (!bubbleState) return;
    revision++;
    bubbleState.revision = revision;
    reqID = "";
    cardRequests.clear();
    void TranslateSvc.cancel(bubbleState.sessionID, revision).catch((err) => toast(String(err)));
  }

  function stopRenderedRun(stoppedRevision: number): void {
    revision = stoppedRevision;
    if (bubbleState) bubbleState.revision = revision;
    reqID = "";
    cardRequests.clear();
    if (bubbleState?.phase === "recognizing") {
      bubbleState.phase = "error";
      bubbleState.message = "已取消识别，可重新框选或直接输入。";
    }
    for (const [name, card] of cards) {
      if (card.status === "loading" || card.status === "streaming") {
        cards.set(name, { status: "error", text: card.text, err: "翻译已取消，请重试" });
      }
    }
  }

  function closeBubble(): void {
    if (!bubbleState) return;
    const session = bubbleState.sessionID;
    const show = bubbleState.showID;
    const closingRevision = ++revision;
    bubbleState.revision = closingRevision;
    void BubbleSvc.close(bubbleID, session, show, closingRevision)
      .then((accepted) => {
        if (accepted === true && bubbleState?.sessionID === session && bubbleState.showID === show && revision <= closingRevision) {
          stopRenderedRun(closingRevision);
        }
      })
      .catch((err) => toast(String(err)));
  }

  function togglePin(): void {
    if (!bubbleState) return;
    if (bubbleState.pinned) {
      bubbleState.pinned = false;
      void BubbleSvc.unpin(bubbleID);
    } else {
      bubbleState.pinned = true;
      void BubbleSvc.pin(bubbleID);
    }
  }

  function run(service = ""): void {
    const text = inputText.trim();
    if (!bubbleState) return;
    if (!text) {
      bubbleState.phase = "";
      bubbleState.message = "";
      cancelCurrent();
      runServices = null;
      cards.clear();
      submittedText = "";
      return;
    }
    if (service && text !== submittedText) service = "";
    reqID = requestID();
    revision++;
    bubbleState.revision = revision;
    const request = reqID;
    retryService = service;
    submittedText = text;
    bubbleState.phase = "";
    bubbleState.message = "";
    detected = "";

    if (!service) {
      if (scrollElem) scrollElem.scrollTop = 0;
      runServices = null;
      cards.clear();
      cardRequests.clear();
    }
    for (const name of service ? [service] : activeServices(bubbleState.settings)) {
      cardRequests.set(name, reqID);
      cards.set(name, { status: "loading" });
    }

    void TranslateSvc.run({
      reqID,
      sessionID: bubbleState.sessionID,
      revision,
      service,
      source: bubbleState.trigger,
      text,
      sourceLang,
      targetLang,
    }).catch((err) => {
      for (const [name, card] of cards) {
        if ((card.status === "loading" || card.status === "streaming") && cardRequests.get(name) === request) {
          cards.set(name, { status: "error", text: card.text, err: String(err) });
        }
      }
    });
  }

  async function copyText(text: string, successMessage = "已复制"): Promise<void> {
    if (!text.trim()) return;
    try {
      await BubbleSvc.copy(text);
      toast(successMessage);
    } catch (err) {
      toast(String(err));
    }
  }

  function speakText(text: string, lang: string): void {
    try {
      window.speechSynthesis.cancel();
      const utter = new SpeechSynthesisUtterance(text);
      utter.lang = speechTag(lang);
      window.speechSynthesis.speak(utter);
    } catch {
      toast("朗读不可用");
    }
  }

  function toggleCardCollapse(name: string): void {
    const next = !collapsedCards[name];
    collapsedCards[name] = next;
    sessionStorage.setItem(`collapse:${name}`, next ? "1" : "0");
  }

  function openTranslationSettings(): void {
    void BubbleSvc.openTranslationSettings().catch((err) => toast(String(err)));
  }

  async function recoverClipboard(id: string, restore: boolean): Promise<void> {
    if (pendingRecovery) return;
    pendingRecovery = id;
    try {
      if (restore) await BubbleSvc.restoreClipboard(id);
      else await BubbleSvc.discardClipboardRecovery(id);
      if (bubbleState?.clipboardRecoveryID !== id) return;
      bubbleState.clipboardRecoveryID = "";
      if (bubbleState.message.startsWith("剪贴板未完整恢复")) {
        bubbleState.message = restore
          ? "剪贴板已恢复，可继续翻译。"
          : "已放弃恢复之前的内容，当前剪贴板没有改变，可继续取词。";
      }
      if (restore) toast("剪贴板已恢复");
    } catch (err) {
      toast(String(err));
    } finally {
      pendingRecovery = "";
    }
  }

  onMount(() => {
    bubbleID = propID ?? Number(new URLSearchParams(location.search).get("b") || "0");

    for (const name of Object.keys(SERVICE_NAMES)) {
      collapsedCards[name] = sessionStorage.getItem(`collapse:${name}`) === "1";
    }

    window.addEventListener("resize", reportLayout);

    let ready = false;
    let disposed = false;
    const queued: (() => void)[] = [];
    function afterReady(handle: () => void): void {
      if (ready) handle();
      else queued.push(handle);
    }

    const subscriptions = [
      events.bubbleLayoutChanged.on((height) => {
        afterReady(() => { maxHeight = height; });
      }),
      events.runStopped.on((msg) => {
        afterReady(() => {
          if (!bubbleState || msg.sessionID !== bubbleState.sessionID || msg.showID !== bubbleState.showID || msg.revision < revision) return;
          stopRenderedRun(msg.revision);
        });
      }),
      events.bubblePresented.on((msg) => {
        afterReady(() => {
          if (!bubbleState || msg.sessionID !== bubbleState.sessionID || msg.showID < bubbleState.showID) return;
          if (msg.showID !== bubbleState.showID) {
            cancelLayout();
            bubbleState.showID = msg.showID;
            void tick().then(reportLayout);
          }
          bubbleState.clipboardRecoveryID = msg.clipboardRecoveryID;
          if (msg.revision > revision) stopRenderedRun(msg.revision);
          if (msg.focus) textareaElem?.focus();
        });
      }),
      events.bubbleReset.on((st) => {
        afterReady(() => {
          if (st.id !== bubbleID) return;
          apply(st);
        });
      }),
      events.runMeta.on((msg: RunMetaMsg) => {
        afterReady(() => {
          if (msg.reqID !== reqID) {
            for (const [name, request] of cardRequests) {
              if (request === msg.reqID && !msg.services?.includes(name)) {
                cards.set(name, { status: "error", err: "该服务当前不可用，请检查设置" });
              }
            }
            return;
          }
          if (bubbleState) bubbleState.revision = revision;
          sourceLang = msg.sourceLang;
          targetLang = msg.targetLang;
          if (!retryService) {
            runServices = new Set(msg.services ?? []);
            for (const name of runServices) cardRequests.set(name, msg.reqID);
            for (const name of cards.keys()) {
              if (!runServices.has(name)) cards.delete(name);
            }
          } else if (!msg.services?.includes(retryService)) {
            cards.set(retryService, { status: "error", err: "该服务当前不可用，请检查设置" });
          }
        });
      }),
      events.translationUpdate.on((msg: TranslationUpdateMsg) => {
        afterReady(() => {
          if (msg.reqID !== (cardRequests.get(msg.service) ?? reqID)) return;
          if (
            sourceLang === "auto" &&
            msg.detectedLang &&
            msg.detectedLang !== "latin" &&
            msg.detectedLang !== "unknown"
          ) {
            if (!detected) detected = msg.detectedLang;
          }
          applyResult(msg);
        });
      }),
      events.settingsChanged.on((s: Settings) => {
        afterReady(() => {
          if (bubbleState) bubbleState.settings = s;
        });
      })
    ];

    // Apply the snapshot first so queued events can be checked against its session and revision.
    void BubbleSvc.ready(bubbleID)
      .then((snapshot) => {
        if (!disposed) apply(snapshot);
      })
      .catch(() => {
        if (!disposed) readyFailed = true;
      })
      .finally(() => {
        if (disposed) return;
        ready = true;
        for (const handle of queued) handle();
        queued.length = 0;
      });

    return () => {
      disposed = true;
      cancelLayout();
      window.removeEventListener("resize", reportLayout);
      for (const off of subscriptions) off();
      queued.length = 0;
      bubbleState = null;
      cards.clear();
      cardRequests.clear();
    };
  });
</script>

<svelte:document onvisibilitychange={() => { pageVisible = !document.hidden; }} onkeydown={(e) => {
  if (e.key === "Escape" && !e.isComposing) closeBubble();
}} />

{#if readyFailed}
  <div class="bubble-error" role="status">气泡已关闭</div>
{:else}
  <div bind:this={bubbleElem} use:observeLayout class="bubble" class:pinned={isPinned}
    style:--bubble-max-height={`${maxHeight}px`}
  >
    <!-- Top Bar -->
    <div class="bubble-top">
      <span class="brand">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <rect x="2.5" y="2.8" width="19" height="16.8" rx="5.2" fill="currentColor"/>
          <path d="M7 19.2 5.8 22.4 9 20.6" fill="currentColor"/>
          <path d="M15 8.2C14.3 7 12.4 6.8 11.25 7.5C9.84 8.34 9.6 9.73 10.9 10.6C12.07 11.39 13.6 11.39 14.3 12.2C15.23 13.24 14.65 15.23 13 15.8C11.72 16.3 10.31 15.8 9.49 14.88" fill="none" stroke="var(--panel)" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" transform="translate(12 11.7) scale(1.12) translate(-12 -11.7)"/>
        </svg>
      </span>
      <div class="top-actions">
        <button
          type="button"
          class="icon-btn pin-btn"
          disabled={isRecognizing}
          title={isPinned ? "已钉住：点击取消，气泡恢复失焦隐藏" : "钉住：失焦不隐藏，之后的新翻译另开气泡"}
          aria-label="钉住"
          aria-pressed={isPinned}
          onclick={togglePin}
        >
          <BubbleIcon name="pin" filled={isPinned} />
        </button>
        <button
          type="button"
          class="icon-btn close-btn"
          title="关闭"
          aria-label="关闭"
          onclick={closeBubble}
        >
          <BubbleIcon name="close" />
        </button>
      </div>
    </div>

    <!-- Keep the header and its native drag region outside the scroller. -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex (The scrollable content needs keyboard access.) -->
    <div class="bubble-body bubble-scroll" bind:this={scrollElem} use:scrollActivity
      role="region" aria-label="翻译内容" tabindex="0"
    >
      <!-- Capture Status & Clipboard Recovery -->
      {#if statusMessage || isRecognizing || bubbleState?.clipboardRecoveryID}
        <div class="capture-status" role="status" aria-atomic="true">
          {#if statusMessage}
            <span class="capture-message">
              {#if isRecognizing}
                <span class="ocr-loading" class:paused={!pageVisible} aria-hidden="true"><span></span><span></span><span></span></span>
              {/if}
              <span>{statusMessage}</span>
            </span>
          {/if}

          {#if isRecognizing}
            <button
              type="button"
              class="text-btn"
              onclick={() => {
                cancelCurrent();
                if (bubbleState) {
                  bubbleState.phase = "error";
                  bubbleState.message = "已取消识别，可重新框选或直接输入。";
                }
                textareaElem?.focus();
              }}
            >
              取消识别
            </button>
          {:else if bubbleState?.trigger === "ocr" || bubbleState?.phase === "error"}
            <button
              type="button"
              class="text-btn"
              onclick={() => void SnipSvc.start().catch((err) => toast(String(err)))}
            >
              {bubbleState?.trigger === "ocr" ? "重新框选" : "截图翻译"}
            </button>
          {/if}

          {#if bubbleState?.clipboardRecoveryID}
            {@const recovery = bubbleState.clipboardRecoveryID}
            <span class="capture-message">
              之前复制的内容尚未恢复，部分应用的划词翻译已暂停。请重试恢复或放弃恢复，也可直接输入或截图翻译。退出应用后，这些内容将无法恢复。
            </span>
            <button
              type="button"
              class="text-btn"
              disabled={Boolean(pendingRecovery)}
              onclick={() => void recoverClipboard(recovery, true)}
            >
              重试恢复剪贴板
            </button>
            <button
              type="button"
              class="text-btn"
              disabled={Boolean(pendingRecovery)}
              onclick={() => void recoverClipboard(recovery, false)}
            >
              放弃恢复，继续取词
            </button>
          {/if}
        </div>
      {/if}

      <!-- Composer -->
      <div class="composer" aria-busy={isRecognizing}>
        <textarea
          bind:this={textareaElem}
          value={inputText}
          class="bubble-input"
          use:scrollActivity
          rows="1"
          spellcheck="false"
          placeholder={isRecognizing ? "识别完成后可修改原文" : "输入要翻译的文本，回车翻译…"}
          title="可直接修改，Enter 翻译，Shift+Enter 换行"
          aria-label="要翻译的原文"
          readonly={isRecognizing}
          oninput={(e) => {
            inputText = e.currentTarget.value;
            growTextarea();
            if (!inputText.trim()) run();
          }}
          onkeydown={(e) => {
            if (e.key === "Enter" && !e.isComposing && !e.shiftKey) {
              e.preventDefault();
              run();
            }
          }}
        ></textarea>

        <div class="composer-actions">
          <button
            type="button"
            class="icon-btn"
            title="朗读输入"
            aria-label="朗读输入"
            onclick={() => {
              const t = inputText.trim();
              if (t) speakText(t, detected || sourceLang);
            }}
          >
            <BubbleIcon name="speak" />
          </button>

          <button
            type="button"
            class="icon-btn"
            title="复制输入"
            aria-label="复制输入"
            onclick={() => void copyText(inputText)}
          >
            <BubbleIcon name="copy" />
          </button>

          {#if detectedDisplay}
            <div class="badge">
              <span class="badge-label">识别为</span>
              <span class="badge-lang">{detectedDisplay}</span>
            </div>
          {/if}

          <button
            type="button"
            class="clear-btn"
            title="清空输入"
            aria-label="清空输入"
            onclick={() => {
              inputText = "";
              if (bubbleState) {
                bubbleState.phase = "";
                bubbleState.message = "";
              }
              void tick().then(growTextarea);
              run();
              textareaElem?.focus();
            }}
          >
            <BubbleIcon name="close" />
          </button>
        </div>
      </div>

      <!-- Language Bar -->
      <div class="language-bar">
        <label class="lang-field">
          <span class="lang-value" aria-hidden="true">{detectedName(sourceLang)}</span>
          <span class="lang-caret" aria-hidden="true">
            <BubbleIcon name="chevron" />
          </span>
          <select
            value={sourceLang}
            class="source-select"
            disabled={isRecognizing}
            aria-label="源语言"
            onchange={(e) => {
              sourceLang = e.currentTarget.value;
              detected = "";
              if (inputText.trim()) run();
            }}
          >
            <option value="auto">自动检测</option>
            {#each LANGUAGES as l (l.code)}
              <option value={l.code}>{l.name}</option>
            {/each}
            {#if sourceLang && sourceLang !== "auto" && !LANGUAGES.some((l) => l.code === sourceLang)}
              <option value={sourceLang}>{detectedName(sourceLang)}</option>
            {/if}
          </select>
        </label>

        <button
          type="button"
          class="icon-btn swap-btn"
          title="交换语言"
          aria-label="交换语言"
          onclick={swapLanguages}
        >
          <BubbleIcon name="swap" />
        </button>

        <label class="lang-field">
          <span class="lang-value" aria-hidden="true">{langName(targetLang)}</span>
          <span class="lang-caret" aria-hidden="true">
            <BubbleIcon name="chevron" />
          </span>
          <select
            value={targetLang}
            class="lang-select"
            disabled={isRecognizing}
            aria-label="目标语言"
            onchange={(e) => {
              targetLang = e.currentTarget.value;
              if (inputText.trim()) run();
            }}
          >
            {#each LANGUAGES as l (l.code)}
              <option value={l.code}>{l.name}</option>
            {/each}
            {#if targetLang && !LANGUAGES.some((l) => l.code === targetLang)}
              <option value={targetLang}>{targetLang}</option>
            {/if}
          </select>
        </label>
      </div>

      <!-- Cards List -->
      {#if enabledServices.length === 0}
        <div class="bubble-cards">
          <div class="cards-empty">
            <span>没有可用的翻译服务</span>
            <button type="button" class="text-btn" onclick={openTranslationSettings}>
              配置翻译服务
            </button>
          </div>
        </div>
      {:else if showingServices.length > 0}
        <div class="bubble-cards">
          {#each showingServices as name (name)}
            {@const card = cards.get(name)}
            {@const isCollapsed = Boolean(collapsedCards[name])}
            {#if card}
              <TranslationCard
                {name}
                title={SERVICE_NAMES[name] ?? name}
                {card}
                {isCollapsed}
                onToggle={() => toggleCardCollapse(name)}
                onRetry={() => run(name)}
                onOpenSettings={openTranslationSettings}
                onSpeak={(text) => speakText(text, targetLang)}
                onCopy={copyText}
              />
            {/if}
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}

<ToastStack {messages} />
