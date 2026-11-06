import {
  BubbleSvc,
  SnipSvc,
  TranslateSvc,
  events,
  type BubbleState,
  type FormatOption,
  type RunMetaMsg,
  type Settings,
  type TranslationUpdateMsg,
} from "../mygo";
import { detectedName, speechTag, LANGUAGES, SERVICE_NAMES } from "../languages";
import { el, requestID, toast } from "../ui";

// One service card's state.
interface CardState {
  status: "loading" | "ok" | "error";
  text?: string;
  err?: string;
  formats?: FormatOption[];
}

// The icons, as inline SVG so they follow the theme, everywhere the
// same. (Emoji render inconsistently in WebView2.)
const ICONS = {
  pin: `<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z"/>`,
  close: `<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`,
  swap: `<path d="m16 3 4 4-4 4"/><path d="M20 7H4"/><path d="m8 21-4-4 4-4"/><path d="M4 17h16"/>`,
  speak: `<path d="M11 5 6 9H2v6h4l5 4V5z"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/>`,
  copy: `<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`,
  chevron: `<path d="m6 9 6 6 6-6"/>`,
};

function svg(body: string, filled = false): string {
  return `<svg viewBox="0 0 24 24" fill="${filled ? "currentColor" : "none"}" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${body}</svg>`;
}

function iconButton(icon: keyof typeof ICONS, title: string, className = ""): HTMLButtonElement {
  const btn = el("button", `icon-btn ${className}`.trim());
  btn.innerHTML = svg(ICONS[icon]);
  btn.title = title;
  btn.setAttribute("aria-label", title);
  return btn;
}

// The page's own state, on top of what Go pushed through Ready and the
// events. The textarea owns the draft; unchanged cards retain their DOM nodes.
let bubbleID = 0;
let state: BubbleState | null = null;
let reqID = ""; // the run the cards are showing
let runServices: Set<string> | null = null;
let sourceLang = "auto";
let targetLang = "zh-HANS";
let detected = ""; // the concrete language a service reported, if any
let revision = 0;
let submittedText = "";
let retryService = "";
const cardNodes = new Map<string, { state: CardState; node: HTMLElement }>();
let statusHost: HTMLDivElement;
let resetVersion = 0;
let mounted = false;
let queued: (() => void)[] = [];
let subscriptions: (() => void)[] = [];
let pageListeners: AbortController | undefined;
const cards = new Map<string, CardState>();
const cardRequests = new Map<string, string>();

// DOM handles, built once.
let input: HTMLTextAreaElement;
let badge: HTMLDivElement;
let sourceSelect: HTMLSelectElement;
let targetSelect: HTMLSelectElement;
let cardsHost: HTMLDivElement;
let pinButton: HTMLButtonElement;
let closeButton: HTMLButtonElement;
let bubbleRoot: HTMLDivElement;

/** renderBubble mounts the bubble page. */
export async function renderBubble(root: HTMLElement): Promise<void> {
  disposeBubble();
  pageListeners = new AbortController();
  const { signal } = pageListeners;
  const params = new URLSearchParams(location.hash.split("?")[1] ?? "");
  bubbleID = Number(params.get("b") ?? "0");

  build(root);
  wireEvents(signal);

  try {
    const version = resetVersion;
    const snapshot = await BubbleSvc.ready(bubbleID);
    if (signal.aborted) return;
    if (version === resetVersion) apply(snapshot);
    mounted = true;
    for (const event of queued) event();
    queued = [];
  } catch {
    if (signal.aborted) return;
    disposeBubble();
    root.replaceChildren(el("div", "bubble-error", "气泡已关闭"));
  }
}

// Route changes must also release listeners when the next page is not a bubble.
export function disposeBubble(): void {
  pageListeners?.abort();
  pageListeners = undefined;
  for (const off of subscriptions) off();
  subscriptions = []; mounted = false; queued = []; state = null;
  reqID = ""; cards.clear(); cardNodes.clear(); cardRequests.clear();
}

function build(root: HTMLElement): void {
  bubbleRoot = el("div", "bubble");

  const top = el("div", "bubble-top");
  const brand = el("span", "brand");
  // The app icon's speech bubble with an S, at chrome size.
  brand.innerHTML = `<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="2.5" y="2.8" width="19" height="16.8" rx="5.2" fill="currentColor"/><path d="M7 19.2 5.8 22.4 9 20.6" fill="currentColor"/><path d="M15 8.2C14.3 7 12.4 6.8 11.25 7.5C9.84 8.34 9.6 9.73 10.9 10.6C12.07 11.39 13.6 11.39 14.3 12.2C15.23 13.24 14.65 15.23 13 15.8C11.72 16.3 10.31 15.8 9.49 14.88" fill="none" stroke="var(--panel)" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" transform="translate(12 11.7) scale(1.12) translate(-12 -11.7)"/></svg>`;
  const topActions = el("div", "top-actions");
  pinButton = iconButton("pin", "钉住：失焦不隐藏，之后的新翻译另开气泡", "pin-btn");
  closeButton = iconButton("close", "关闭", "close-btn");

  topActions.append(pinButton, closeButton);
  top.append(brand, topActions);

  input = el("textarea", "bubble-input");
  input.rows = 1;
  input.spellcheck = false;
  input.placeholder = "输入要翻译的文本，回车翻译…";
  input.title = "可直接修改，Enter 翻译，Shift+Enter 换行";
  input.setAttribute("aria-label", "要翻译的原文");

  badge = el("div", "badge");
  const composerActions = el("div", "composer-actions");
  const speakInput = iconButton("speak", "朗读输入");
  speakInput.addEventListener("click", () => {
    const text = input.value.trim();
    if (text) speakText(text, detected || sourceLang);
  });
  const copyInput = iconButton("copy", "复制输入");
  copyInput.addEventListener("click", () => {
    const text = input.value.trim();
    if (!text) return;
    void BubbleSvc.copy(input.value);
    toast("已复制");
  });
  const clear = el("button", "clear-btn");
  clear.innerHTML = svg(ICONS.close);
  clear.title = "清空输入";
  clear.setAttribute("aria-label", "清空输入");
  clear.addEventListener("click", () => {
    input.value = "";
    input.readOnly = false;
    if (state) { state.phase = ""; state.message = ""; }
    grow();
    run();
    input.focus();
  });
  composerActions.append(speakInput, copyInput, badge, clear);
  const composer = el("div", "composer");
  composer.append(input, composerActions);

  sourceSelect = el("select", "source-select");
  targetSelect = el("select", "lang-select");
  sourceSelect.setAttribute("aria-label", "源语言");
  targetSelect.setAttribute("aria-label", "目标语言");
  const sourceField = el("label", "lang-field");
  const sourceCaret = el("span", "lang-caret");
  sourceCaret.innerHTML = svg(`<path d="m6 9 6 6 6-6"/>`);
  sourceField.append(sourceCaret, sourceSelect);
  const swap = iconButton("swap", "交换语言", "swap-btn");
  const targetField = el("label", "lang-field");
  const targetCaret = el("span", "lang-caret");
  targetCaret.innerHTML = svg(`<path d="m6 9 6 6 6-6"/>`);
  targetField.append(targetCaret, targetSelect);
  const languageBar = el("div", "language-bar");
  languageBar.append(sourceField, swap, targetField);

  cardsHost = el("div", "bubble-cards");
  const body = el("div", "bubble-body");
  statusHost = el("div", "capture-status");
  statusHost.setAttribute("role", "status");
  body.append(statusHost, composer, languageBar, cardsHost);
  bubbleRoot.append(top, body);
  root.replaceChildren(bubbleRoot);
  swap.addEventListener("click", swapLanguages);
}

function wireEvents(signal: AbortSignal): void {
  input.addEventListener("input", () => {
    grow();
    // Clearing the composer hides the old cards at once; text changes wait
    // for Enter instead of translating while the user is still typing.
    if (!input.value.trim()) run();
    renderStatus();
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.isComposing && !e.shiftKey) {
      e.preventDefault();
      run();
    }
  });
  sourceSelect.addEventListener("change", () => {
    sourceLang = sourceSelect.value;
    detected = "";
    renderMeta();
    if (input.value.trim()) run();
  });
  targetSelect.addEventListener("change", () => {
    targetLang = targetSelect.value;
    run();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if (e.isComposing) return;
    closeBubble();
  }, { signal });
  pinButton.addEventListener("click", () => {
    // Pin keeps the bubble; pinning again lets go of it.
    if (state?.pinned) {
      state.pinned = false;
      void BubbleSvc.unpin(bubbleID);
    } else {
      if (state) state.pinned = true;
      void BubbleSvc.pin(bubbleID);
    }
    renderChrome();
  });
  closeButton.addEventListener("click", closeBubble);

  subscriptions.push(events.runStopped.on((msg) => {
    const handle = () => {
      if (!state || msg.sessionID !== state.sessionID || msg.showID !== state.showID || msg.revision < revision) return;
      stopRenderedRun(msg.revision);
    };
    if (mounted) handle(); else queued.push(handle);
  }));
  subscriptions.push(events.bubblePresented.on((msg) => {
    const handle = () => {
      if (!state || msg.sessionID !== state.sessionID || msg.showID < state.showID) return;
      state.showID = msg.showID;
      state.clipboardRecoveryID = msg.clipboardRecoveryID;
      // Native close may have completed while this page was hidden.
      if (msg.revision > revision) stopRenderedRun(msg.revision);
      renderStatus();
      if (msg.focus) input.focus();
    };
    if (mounted) handle(); else queued.push(handle);
  }));
  subscriptions.push(events.bubbleReset.on((st) => {
    const handle = () => {
      if (st.id !== bubbleID) return;
      resetVersion++;
      apply(st);
    };
    if (mounted) handle(); else queued.push(handle);
  }));
  subscriptions.push(events.runMeta.on((msg: RunMetaMsg) => {
    const handle = () => {
      if (msg.reqID !== reqID) {
        // A peer retry can still be active after another card was retried.
        for (const [name, request] of cardRequests) {
          if (request === msg.reqID && !msg.services?.includes(name)) {
            cards.set(name, {status:"error", err:"该服务当前不可用，请检查设置"});
          }
        }
        renderCards();
        return;
      }
      if (state) state.revision = revision;
      sourceLang = msg.sourceLang;
      targetLang = msg.targetLang;
      // Services without credentials never answer: drop their cards
      // instead of shimmering forever.
      if (!retryService) {
        runServices = new Set(msg.services ?? []);
        for (const name of runServices) cardRequests.set(name, msg.reqID);
        for (const name of cards.keys()) { if (!runServices.has(name)) cards.delete(name); }
      } else if (!msg.services?.includes(retryService)) {
        cards.set(retryService, {status:"error", err:"该服务当前不可用，请检查设置"});
      }
      renderMeta();
      renderCards();
    };
    if (mounted) handle(); else queued.push(handle);
  }));
  subscriptions.push(events.translationUpdate.on((msg: TranslationUpdateMsg) => {
    const handle = () => {
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
      renderCards();
      renderMeta();
    };
    if (mounted) handle(); else queued.push(handle);
  }));
  subscriptions.push(events.settingsChanged.on((s: Settings) => {
    const handle = () => {
      if (state) state.settings = s;
      renderCards();
    };
    if (mounted) handle(); else queued.push(handle);
  }));
}

/** apply takes a fresh state from Go, whether on mounting or reuse. */
function apply(st: BubbleState): void {
  if (state && (st.generation < state.generation ||
    (st.sessionID === state.sessionID && (st.revision < state.revision || st.showID < state.showID)))) return;
  state = st;
  reqID = st.reqID;
  runServices = null;
  detected = "";
  revision = Math.max(0, st.revision);
  submittedText = st.text;
  retryService = "";
  sourceLang = st.sourceLang || "auto";
  targetLang = st.targetLang || "zh-HANS";
  input.value = st.text;
  input.readOnly = st.phase === "recognizing";
  cards.clear();
  cardNodes.clear();
  cardRequests.clear();
  for (const [service, id] of Object.entries(st.serviceRequests ?? {})) cardRequests.set(service, id);
  if (st.reqID && st.text) {
    // A selection or OCR query runs from Go; show its cards loading.
    for (const name of activeServices(st.settings)) {
      cards.set(name, { status: "loading" });
    }
  }
  if (st.meta && st.meta.reqID === st.reqID) {
    sourceLang = st.meta.sourceLang; targetLang = st.meta.targetLang;
    runServices = new Set(st.meta.services ?? []);
    for (const name of cards.keys()) { if (!runServices.has(name)) cards.delete(name); }
  }
  for (const msg of st.results ?? []) {
    applyResult(msg);
    if (msg.detectedLang && !detected) detected = msg.detectedLang;
  }
  grow();
  renderStatus();
  renderMeta();
  renderCards();
  renderChrome();
  if (st.trigger === "input") {
    input.focus();
  }
}

// Snapshots and live results must expose the same result actions.
function applyResult(msg: TranslationUpdateMsg): void {
  cards.set(msg.service, msg.err ? {status:"error", err:msg.err} : {status:"ok", text:msg.text});
  if (!msg.err && targetLang === "en" && msg.text && msg.service !== "dictionary") {
    void attachFormats(msg.service, msg.text);
  }
}

/** swapLanguages turns the detected source into the target and back. */
function swapLanguages(): void {
  const from = detected || sourceLang;
  if (!from || from === "auto" || from === "latin" || from === "unknown") {
    toast("先输入文本，检测到源语言后再交换");
    return;
  }
  const fromCode = from === "zh" ? "zh-HANS" : from;
  const previousTarget = targetLang;
  targetLang = fromCode;
  sourceLang = previousTarget;
  detected = "";
  renderMeta();
  run();
}

function cancelCurrent(): void {
  if (!state) return;
  revision++;
  state.revision = revision;
  reqID = "";
  cardRequests.clear();
  void TranslateSvc.cancel(state.sessionID, revision).catch((err) => toast(String(err)));
}

function stopRenderedRun(stoppedRevision: number): void {
  revision = stoppedRevision;
  if (state) state.revision = revision;
  reqID = ""; cardRequests.clear();
  if (state?.phase === "recognizing") {
    state.phase = "error"; state.message = "已取消识别，可重新框选或直接输入。";
    input.readOnly = false;
  }
  for (const [name, card] of cards) {
    if (card.status === "loading") cards.set(name, {status:"error", err:"翻译已取消，请重试"});
  }
  renderCards(); renderStatus();
}

function closeBubble(): void {
  if (!state) return;
  const session = state.sessionID, show = state.showID, closingRevision = revision + 1;
  stopRenderedRun(closingRevision);
  // The backend validates presentation and revision before canceling anything.
  void BubbleSvc.close(bubbleID, session, show, closingRevision).catch((err) => toast(String(err)));
}

function renderStatus(): void {
  const recognizing = state?.phase === "recognizing";
  pinButton.disabled = recognizing;
  sourceSelect.disabled = recognizing;
  targetSelect.disabled = recognizing;
  input.placeholder = recognizing ? "识别完成后可修改原文" : "输入要翻译的文本，回车翻译…";
  statusHost.replaceChildren();
  const dirty = !!submittedText && input.value.trim() !== submittedText.trim();
  const message = state?.phase === "recognizing" ? "正在识别文字…" :
    dirty ? "原文已修改，按 Enter 重新翻译" : state?.message ||
    (state?.trigger === "ocr" ? "识别文字可直接修改，按 Enter 重新翻译" : "");
  if (message) statusHost.append(el("span", "capture-message", message));
  if (state?.phase === "recognizing") {
    const cancel = el("button", "text-btn", "取消识别");
    cancel.addEventListener("click", () => {
      cancelCurrent();
      if (state) { state.phase = "error"; state.message = "已取消识别，可重新框选或直接输入。"; }
      input.readOnly = false; renderStatus(); input.focus();
    });
    statusHost.append(cancel);
  } else if (state?.trigger === "ocr" || state?.phase === "error") {
    const retry = el("button", "text-btn", state.trigger === "ocr" ? "重新框选" : "截图翻译");
    retry.addEventListener("click", () => { void SnipSvc.start().catch((err) => toast(String(err))); });
    statusHost.append(retry);
  }
  if (state?.clipboardRecoveryID) {
    const recovery = state.clipboardRecoveryID;
    statusHost.append(el("span", "capture-message", "之前复制的内容尚未恢复，部分应用的划词翻译已暂停。请重试恢复或放弃恢复，也可直接输入或截图翻译。退出应用后，这些内容将无法恢复。"));
    const restore = el("button", "text-btn", "重试恢复剪贴板");
    restore.addEventListener("click", async () => {
      restore.disabled = true;
      try {
        await BubbleSvc.restoreClipboard(recovery);
        if (state?.clipboardRecoveryID === recovery) {
          state.clipboardRecoveryID = "";
          if (state.message.startsWith("剪贴板未完整恢复")) state.message = "剪贴板已恢复，可继续翻译。";
        }
        toast("剪贴板已恢复"); renderStatus();
      } catch (err) { toast(String(err)); restore.disabled = false; }
    });
    const discard = el("button", "text-btn", "放弃恢复，继续取词");
    discard.addEventListener("click", async () => {
      try {
        await BubbleSvc.discardClipboardRecovery(recovery);
        if (state?.clipboardRecoveryID === recovery) {
          state.clipboardRecoveryID = "";
          if (state.message.startsWith("剪贴板未完整恢复")) state.message = "已放弃恢复之前的内容，当前剪贴板没有改变，可继续取词。";
        }
        renderStatus();
      } catch (err) { toast(String(err)); }
    });
    statusHost.append(restore, discard);
  }
  statusHost.classList.toggle("hidden", statusHost.children.length === 0);
}

function run(service = ""): void {
  const text = input.value.trim();
  if (!state) return;
  if (!text) {
    state.phase = ""; state.message = ""; input.readOnly = false;
    cancelCurrent(); runServices = null; cards.clear(); submittedText = "";
    renderCards(); renderStatus(); return;
  }
  // Retrying a result is valid only for the same unmodified input.
  if (service && text !== submittedText) service = "";
  reqID = requestID();
  revision++;
  state.revision = revision;
  const request = reqID;
  retryService = service;
  submittedText = text;
  state.phase = ""; state.message = "";
  detected = "";
  renderMeta(); renderStatus();
  if (!service) { runServices = null; cards.clear(); cardRequests.clear(); }
  for (const name of service ? [service] : activeServices(state.settings)) {
    cardRequests.set(name, reqID);
    cards.set(name, {status:"loading"});
  }
  renderCards();
  void TranslateSvc.run({reqID, sessionID:state.sessionID, revision, service,
    source:state.trigger, text, sourceLang, targetLang}).catch((err) => {
      for (const [name, card] of cards) {
        if (card.status === "loading" && cardRequests.get(name) === request) cards.set(name, {status:"error", err:String(err)});
      }
      renderCards();
    });
}

function activeServices(settings: Settings): string[] {
  return settings.services.order.filter((name) => settings.services.enabled.includes(name));
}

function grow(): void {
  input.style.height = "auto";
  // Keep the composer readable for a paragraph or two, then scroll: the
  // cards below need the rest of the bubble.
  input.style.height = `${Math.min(input.scrollHeight, 240)}px`;
}

function renderChrome(): void {
  const pinned = state?.pinned ?? false;
  bubbleRoot.classList.toggle("pinned", pinned);

  pinButton.innerHTML = svg(ICONS.pin, pinned);
  pinButton.title = pinned
    ? "已钉住：点击取消，气泡恢复失焦隐藏"
    : "钉住：失焦不隐藏，之后的新翻译另开气泡";
}

function renderMeta(): void {
  if (sourceSelect.options.length === 0) {
    const auto = el("option", undefined, "自动检测");
    auto.value = "auto";
    sourceSelect.append(auto);
    for (const l of LANGUAGES) {
      const opt = el("option", undefined, l.name);
      opt.value = l.code;
      sourceSelect.append(opt);
    }
  }
  sourceSelect.value = sourceLang;
  if (!sourceSelect.value) {
    const opt = el("option", undefined, detectedName(sourceLang));
    opt.value = sourceLang;
    sourceSelect.append(opt);
    sourceSelect.value = sourceLang;
  }

  if (targetSelect.options.length === 0) {
    for (const l of LANGUAGES) {
      const opt = el("option", undefined, l.name);
      opt.value = l.code;
      targetSelect.append(opt);
    }
  }
  targetSelect.value = targetLang;
  if (!targetSelect.value) {
    // A target outside the list (should not happen) keeps a raw entry.
    const opt = el("option", undefined, targetLang);
    opt.value = targetLang;
    targetSelect.append(opt);
    targetSelect.value = targetLang;
  }

  const src = detected || sourceLang;
  badge.replaceChildren();
  if (src && src !== "auto" && src !== "latin" && src !== "unknown") {
    badge.classList.remove("hidden");
    badge.classList.remove("muted");
    badge.append(
      el("span", "badge-label", "识别为"),
      el("span", "badge-lang", detectedName(src)),
    );
  } else {
    badge.classList.add("hidden");
    badge.classList.add("muted");
  }
}

function renderCards(): void {
  if (!state) return;
  const enabled = activeServices(state.settings).filter((name) => !runServices || runServices.has(name));
  if (enabled.length === 0) {
    cardsHost.replaceChildren(); cardNodes.clear();
    cardsHost.classList.remove("hidden");
    const empty = el("div", "cards-empty", "没有可用的翻译服务");
    empty.append(translationSettingsButton());
    cardsHost.append(empty);
    return;
  }
  const showing = enabled.filter((name) => cards.has(name));
  if (showing.length === 0) {
    // Nothing typed (or nothing to answer yet): keep the bubble down to
    // the composer and the language row.
    cardsHost.classList.add("hidden");
    cardsHost.replaceChildren(); cardNodes.clear();
    return;
  }
  cardsHost.classList.remove("hidden");
  for (const [name, entry] of cardNodes) {
    if (!showing.includes(name)) { entry.node.remove(); cardNodes.delete(name); }
  }
  // Keep untouched nodes in place: moving them also disrupts browser selection.
  if (cardNodes.size === 0) cardsHost.replaceChildren();
  let previous: HTMLElement | null = null;
  for (const name of showing) {
    const card = cards.get(name)!;
    let entry = cardNodes.get(name);
    if (!entry || entry.state !== card) {
      const node = renderCard(name, card);
      if (entry) { cardsHost.insertBefore(node, entry.node); entry.node.remove(); }
      entry = {state:card, node}; cardNodes.set(name, entry);
    }
    const expected: ChildNode | null = previous ? previous.nextSibling : cardsHost.firstChild;
    if (expected !== entry.node) cardsHost.insertBefore(entry.node, expected);
    previous = entry.node;
  }
}

function translationSettingsButton(): HTMLButtonElement {
  const button = el("button", "text-btn", "配置翻译服务");
  button.addEventListener("click", () => {
    void BubbleSvc.openTranslationSettings().catch((err) => toast(String(err)));
  });
  return button;
}

function renderCard(name: string, card: CardState): HTMLElement {
  const node = el("div", `card card-${card.status}`);
  const head = el("div", "card-head");
  head.append(el("span", "card-name", SERVICE_NAMES[name] ?? name));
  const toggle = iconButton("chevron", "折叠", "card-toggle");
  head.append(toggle);
  const body = el("div", "card-body");

  const collapsed = sessionStorage.getItem(`collapse:${name}`) === "1";
  node.classList.toggle("collapsed", collapsed);
  toggle.setAttribute("aria-expanded", String(!collapsed));
  toggle.setAttribute("aria-label", `${collapsed ? "展开" : "折叠"}${SERVICE_NAMES[name] ?? name}`);
  toggle.addEventListener("click", () => {
    const now = !node.classList.contains("collapsed");
    node.classList.toggle("collapsed", now);
    sessionStorage.setItem(`collapse:${name}`, now ? "1" : "0");
    toggle.setAttribute("aria-expanded", String(!now));
  });

  switch (card.status) {
    case "loading":
      body.append(el("div", "skeleton"), el("div", "skeleton short"));
      break;
    case "error": {
      body.append(el("div", "card-error", card.err ?? "出错了"));
      const retry = el("button", "text-btn", "重试");
      retry.addEventListener("click", () => run(name));
      const actions = el("div", "card-error-actions");
      actions.append(retry);
      if (name === "google" || name === "index-translate") actions.append(translationSettingsButton());
      body.append(actions);
      break;
    }
    case "ok": {
      const scroll = el("div", "card-scroll");
      scroll.append(el("div", "card-text", card.text ?? ""));
      body.append(scroll);
      const tools = el("div", "card-tools");
      if (card.formats?.length) {
        for (const f of card.formats) {
          // The naming styles are icons; the Chinese label (and the text
          // that would be copied) lives in the hover title.
          const btn = el("button", "format-btn");
          btn.append(el("span", "format-icon", formatIcon(f.style)));
          btn.title = `${f.label}：${f.value}`;
          btn.addEventListener("click", () => {
            void BubbleSvc.copy(f.value);
            toast(`已复制 ${f.label}`);
          });
          tools.append(btn);
        }
      }
      const speak = iconButton("speak", "朗读");
      speak.addEventListener("click", () => speakText(card.text ?? "", targetLang));
      const copy = iconButton("copy", "复制");
      copy.addEventListener("click", () => {
        void BubbleSvc.copy(card.text ?? "");
        toast("已复制");
      });
      tools.append(speak, copy);
      body.append(tools);
      break;
    }
  }
  node.append(head, body);
  return node;
}

/** formatIcon is the compact glyph shown for a naming style. */
function formatIcon(style: string): string {
  switch (style) {
    case "lowerCamel":
      return "aB";
    case "upperCamel":
      return "AB";
    case "snake":
      return "a_b";
    case "constant":
      return "A_B";
    case "kebab":
      return "a-b";
    default:
      return style.slice(0, 2);
  }
}

/** attachFormats fetches an English result's naming styles for its chips. */
async function attachFormats(service: string, text: string): Promise<void> {
  const card = cards.get(service);
  if (!card || card.text !== text) return;
  try {
    card.formats = await TranslateSvc.formats(text);
  } catch {
    return; // chips are a bonus, not a requirement
  }
  if (cards.get(service) === card && card.text === text) {
    const entry = cardNodes.get(service);
    if (entry?.state === card) {
      const fresh = renderCard(service, card).querySelector(".card-tools");
      const tools = entry.node.querySelector(".card-tools");
      if (fresh && tools) {
        for (const old of tools.querySelectorAll(".format-btn")) old.remove();
        const before = tools.firstChild;
        for (const format of fresh.querySelectorAll(".format-btn")) tools.insertBefore(format, before);
      }
    }
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

