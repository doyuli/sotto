import {
  BubbleSvc,
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
  return btn;
}

// The page's own state, on top of what Go pushed through Ready and the
// events. The textarea owns the text; cards re-render wholesale.
let bubbleID = 0;
let state: BubbleState | null = null;
let reqID = ""; // the run the cards are showing
let sourceLang = "auto";
let targetLang = "zh-HANS";
let detected = ""; // the concrete language a service reported, if any
let editing = false; // a selection/ocr text turned editable
const cards = new Map<string, CardState>();

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
  const params = new URLSearchParams(location.hash.split("?")[1] ?? "");
  bubbleID = Number(params.get("b") ?? "0");

  build(root);
  wireEvents();

  try {
    apply(await BubbleSvc.ready(bubbleID));
  } catch {
    root.replaceChildren(el("div", "bubble-error", "气泡已关闭"));
  }
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
  closeButton.classList.add("hidden");
  topActions.append(pinButton, closeButton);
  top.append(brand, topActions);

  input = el("textarea", "bubble-input");
  input.rows = 1;
  input.spellcheck = false;
  input.placeholder = "输入要翻译的文本，回车翻译…";
  input.title = "双击可编辑";

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
  clear.addEventListener("click", () => {
    input.value = "";
    grow();
    run();
    input.focus();
  });
  composerActions.append(speakInput, copyInput, badge, clear);
  const composer = el("div", "composer");
  composer.append(input, composerActions);

  sourceSelect = el("select", "source-select");
  targetSelect = el("select", "lang-select");
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
  body.append(composer, languageBar, cardsHost);
  bubbleRoot.append(top, body);
  root.append(bubbleRoot);
  swap.addEventListener("click", swapLanguages);
}

function wireEvents(): void {
  input.addEventListener("input", () => {
    grow();
    // Clearing the composer hides the old cards at once; text changes wait
    // for Enter instead of translating while the user is still typing.
    if (!input.value.trim()) run();
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
    if (editing) {
      editing = false;
      input.readOnly = true;
      e.preventDefault();
      return;
    }
    void BubbleSvc.close(bubbleID);
  });
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
  closeButton.addEventListener("click", () => void BubbleSvc.close(bubbleID));
  bubbleRoot.addEventListener("dblclick", (e) => {
    // A selection or OCR text turns editable on a double click.
    if (!editing && state && state.trigger !== "input" && e.target === input) {
      editing = true;
      input.readOnly = false;
      input.focus();
    }
  });

  events.bubbleReset.on((st) => {
    if (st.id !== bubbleID) return;
    apply(st);
  });
  events.runMeta.on((msg: RunMetaMsg) => {
    if (msg.reqID !== reqID) return;
    sourceLang = msg.sourceLang;
    targetLang = msg.targetLang;
    // Services without credentials never answer: drop their cards
    // instead of shimmering forever.
    if (msg.services?.length) {
      const live = new Set(msg.services);
      for (const name of [...cards.keys()]) {
        if (!live.has(name)) cards.delete(name);
      }
    }
    renderMeta();
    renderCards();
  });
  events.translationUpdate.on((msg: TranslationUpdateMsg) => {
    if (msg.reqID !== reqID) return;
    if (
      sourceLang === "auto" &&
      msg.detectedLang &&
      msg.detectedLang !== "latin" &&
      msg.detectedLang !== "unknown"
    ) {
      detected = msg.detectedLang;
    }
    if (msg.err) {
      cards.set(msg.service, { status: "error", err: msg.err });
    } else {
      cards.set(msg.service, { status: "ok", text: msg.text });
      if (targetLang === "en" && msg.text && msg.service !== "dictionary") {
        void attachFormats(msg.service, msg.text);
      }
    }
    renderCards();
    renderMeta();
  });
  events.settingsChanged.on((s: Settings) => {
    if (state) state.settings = s;
    renderCards();
  });
}

/** apply takes a fresh state from Go, whether on mounting or reuse. */
function apply(st: BubbleState): void {
  state = st;
  reqID = st.reqID;
  detected = "";
  editing = false;
  sourceLang = st.sourceLang || "auto";
  targetLang = st.targetLang || "zh-HANS";
  input.value = st.text;
  input.readOnly = st.trigger !== "input";
  cards.clear();
  if (st.reqID && st.text) {
    // A selection or OCR query runs from Go; show its cards loading.
    for (const name of activeServices(st.settings)) {
      cards.set(name, { status: "loading" });
    }
  }
  grow();
  renderMeta();
  renderCards();
  renderChrome();
  if (st.trigger === "input") {
    input.focus();
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

function run(): void {
  const text = input.value.trim();
  if (!text || !state) {
    cards.clear();
    renderCards();
    return;
  }
  reqID = requestID();
  detected = "";
  renderMeta();
  cards.clear();
  for (const name of activeServices(state.settings)) {
    cards.set(name, { status: "loading" });
  }
  renderCards();
  void TranslateSvc.run({
    reqID,
    source: state.trigger,
    text,
    sourceLang,
    targetLang,
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
  closeButton.classList.toggle("hidden", !pinned);
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
  cardsHost.replaceChildren();
  const enabled = activeServices(state.settings);
  if (enabled.length === 0) {
    cardsHost.classList.remove("hidden");
    cardsHost.append(el("div", "cards-empty", "没有启用的翻译服务，请在设置中开启"));
    return;
  }
  const showing = enabled.filter((name) => cards.has(name));
  if (showing.length === 0) {
    // Nothing typed (or nothing to answer yet): keep the bubble down to
    // the composer and the language row.
    cardsHost.classList.add("hidden");
    return;
  }
  cardsHost.classList.remove("hidden");
  for (const name of showing) {
    cardsHost.append(renderCard(name, cards.get(name) ?? { status: "loading" }));
  }
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
  toggle.addEventListener("click", () => {
    const now = !node.classList.contains("collapsed");
    node.classList.toggle("collapsed", now);
    sessionStorage.setItem(`collapse:${name}`, now ? "1" : "0");
  });

  switch (card.status) {
    case "loading":
      body.append(el("div", "skeleton"), el("div", "skeleton short"));
      break;
    case "error": {
      body.append(el("div", "card-error", card.err ?? "出错了"));
      const retry = el("button", "text-btn", "重试");
      retry.addEventListener("click", () => run());
      body.append(retry);
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
    renderCards();
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

