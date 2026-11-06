// @vitest-environment jsdom
import { expect, test, vi } from "vitest";
import { mount, tick } from "svelte";
import SettingsPage from "../src/components/SettingsPage.svelte";
import type { Settings } from "../src/mygo";
import { deferred, settle, setupPageTest } from "./support/page";

const page = setupPageTest();

function select<T extends Element = HTMLElement>(selector: string): T {
  const node = page.target.querySelector<T>(selector);
  if (!node) throw new Error(`Missing control: ${selector}`);
  return node;
}

async function openSettings(initialTab = "services"): Promise<void> {
  page.track(mount(SettingsPage, { target: page.target, props: { initialTab } }));
  await vi.waitFor(() => expect(page.target.querySelector(".service-list")).not.toBeNull());
}

async function tab(label: string): Promise<void> {
  const button = [...page.target.querySelectorAll<HTMLButtonElement>(".settings-nav button")]
    .find((node) => node.textContent?.trim() === label);
  if (!button) throw new Error(`Missing tab: ${label}`);
  button.click();
  await tick();
}

async function expand(name: string): Promise<void> {
  select<HTMLButtonElement>(`[aria-controls="configure-${name}"]`).click();
  await tick();
}

function change(selector: string, value: string | boolean): void {
  const control = select<HTMLInputElement>(selector);
  if (typeof value === "boolean") control.checked = value;
  else control.value = value;
  control.dispatchEvent(new Event("change", { bubbles: true }));
}

function draft(selector: string, text: string): HTMLInputElement {
  const control = select<HTMLInputElement>(selector);
  control.value = text;
  control.dispatchEvent(new Event("input", { bubbles: true }));
  return control;
}

function keydown(target: EventTarget, key: string, options: KeyboardEventInit = {}): boolean {
  return target.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...options }));
}

function writes(): Settings[] {
  return page.backend.calls("ConfigSvc.Set").map(([settings]) => settings as Settings);
}

const targetLanguage = "#target-language-select";
const openAtLogin = "#open-at-login-switch";
const autoSplit = "#auto-split-switch";
const baiduToggle = '[aria-label="启用百度翻译"]';
const openAIToggle = '[aria-label="启用OpenAI"]';
const moveBaiduToTop = '[aria-label="置顶百度翻译"]';

test("initial services tab and native requests select the service section", async () => {
  await openSettings();
  expect(select(".settings-section.active").id).toBe("section-services");
  expect(select(".service-badge-enabled").textContent).toBe("已启用");

  await tab("通用");
  expect(select(".settings-section.active").id).toBe("section-general");
  page.backend.emit("open-translation-settings", true);
  await tick();
  expect(select(".settings-section.active").id).toBe("section-services");
});

test("Bilibili can be turned off independently of OpenAI", async () => {
  await openSettings();
  const bilibili = '[aria-label="启用Bilibili"]';
  expect(select<HTMLInputElement>(bilibili).checked).toBe(true);
  expect(select<HTMLInputElement>(openAIToggle).disabled).toBe(true);
  change(bilibili, false);
  await settle();
  expect(page.backend.settings.services.enabled).toEqual(["google", "baidu"]);
  expect(select<HTMLInputElement>(openAIToggle).disabled).toBe(true);
});

test("keyboard reorder preserves row focus, drafts and enablement", async () => {
  await openSettings();
  change(baiduToggle, false);
  await settle();
  await expand("openai");
  const credential = draft("#input-openai-key", "unfinished-key");
  const openAIGrip = select('[aria-label="排序OpenAI"]');
  const baiduGrip = select('[aria-label="排序百度翻译"]');

  expect(page.target.querySelector('[aria-label="置顶OpenAI"]')).toBeNull();
  expect(page.target.querySelector(moveBaiduToTop)).toBeNull();
  openAIGrip.focus();
  keydown(openAIGrip, "ArrowUp");
  keydown(openAIGrip, "ArrowUp");
  await settle();
  expect(page.backend.settings.services.order).toEqual(["openai", "index-translate", "google", "baidu", "deepl"]);
  expect(document.activeElement).toBe(openAIGrip);
  expect(select('[aria-controls="configure-openai"]').getAttribute("aria-expanded")).toBe("true");
  expect(credential.value).toBe("unfinished-key");
  expect(select<HTMLInputElement>(openAIToggle).disabled).toBe(true);

  baiduGrip.focus();
  keydown(baiduGrip, "ArrowUp");
  await settle();
  expect(page.backend.settings.services.order).toEqual(["openai", "index-translate", "baidu", "google", "deepl"]);
  expect(page.backend.settings.services.enabled).toEqual(["index-translate", "google"]);
  expect(document.activeElement).toBe(baiduGrip);
  expect(select(".service-order-status").textContent?.trim()).toBe("百度翻译 已移至第 3 位");
});

test("move to top is available only for enabled services and transfers focus to the grip", async () => {
  await openSettings();
  expect(select(moveBaiduToTop).title).toBe("置顶");
  change(baiduToggle, false);
  await settle();
  expect(page.target.querySelector(moveBaiduToTop)).toBeNull();
  change(baiduToggle, true);
  await settle();
  const button = select<HTMLButtonElement>(moveBaiduToTop);
  button.focus();
  button.click();
  await settle();
  expect(page.backend.settings.services.order[0]).toBe("baidu");
  expect(button.disabled).toBe(true);
  expect(document.activeElement).toBe(select('[aria-label="排序百度翻译"]'));
});

function dragEvent(target: Element, type: string, clientY = 0): void {
  const event = new MouseEvent(type, { bubbles: true, cancelable: true, clientY });
  Object.assign(event, { dataTransfer: { setData() {}, effectAllowed: "", dropEffect: "" } });
  target.dispatchEvent(event);
}

test("dropping before the first or after the last row changes order without changing enablement", async () => {
  await openSettings();
  const rows = page.target.querySelectorAll<HTMLElement>(".service-row");
  const grip = select('[aria-label="排序OpenAI"]');
  const first = rows[0]!;
  const last = rows[rows.length - 1]!;
  const rectangle = new DOMRect(0, 0, 100, 80);
  first.getBoundingClientRect = () => rectangle;
  last.getBoundingClientRect = () => rectangle;

  dragEvent(grip, "dragstart");
  dragEvent(first, "dragover", 1);
  await tick();
  expect(first.dataset.drop).toBe("before");
  dragEvent(first, "drop", 1);
  await settle();
  expect(page.backend.settings.services.order).toEqual(["openai", "index-translate", "google", "baidu", "deepl"]);
  expect(first.dataset.drop).toBeUndefined();
  expect(select(".service-list").firstElementChild?.contains(grip)).toBe(true);

  dragEvent(grip, "dragstart");
  dragEvent(last, "dragover", 79);
  await tick();
  expect(last.dataset.drop).toBe("after");
  dragEvent(last, "drop", 79);
  await settle();
  expect(page.backend.settings.services.order).toEqual(["index-translate", "google", "baidu", "deepl", "openai"]);
  expect(page.backend.settings.services.enabled).toEqual(["index-translate", "google", "baidu"]);
  expect(last.dataset.drop).toBeUndefined();
  expect(select(".service-list").lastElementChild?.contains(grip)).toBe(true);
  expect(writes()).toHaveLength(2);
});

test("switching tabs clears a pending drag and unmount releases settings listeners", async () => {
  await openSettings();
  const grip = select('[aria-label="排序OpenAI"]');
  const first = select(".service-row");
  dragEvent(grip, "dragstart");
  dragEvent(first, "dragover");
  await tick();
  expect(select(".service-row.dragging").contains(grip)).toBe(true);
  expect(first.dataset.drop).toBe("before");

  await tab("通用");
  expect(first.dataset.drop).toBeUndefined();
  expect(page.target.querySelector(".service-row.dragging")).toBeNull();
  dragEvent(first, "drop");
  await settle();
  expect(writes()).toHaveLength(0);
  await page.destroy();
  expect([...page.backend.events.values()].every((listeners) => listeners.size === 0)).toBe(true);
});

test("a late initial settings read cannot mount a disposed page", async () => {
  const read = deferred<Settings>();
  page.backend.override("ConfigSvc.Get", () => read.promise);
  page.track(mount(SettingsPage, { target: page.target }));
  await tick();
  expect(page.backend.calls("ConfigSvc.Get")).toHaveLength(1);
  await page.destroy();
  read.resolve(page.backend.settings);
  await settle();
  expect(page.target.children).toHaveLength(0);
  expect([...page.backend.events.values()].every((listeners) => listeners.size === 0)).toBe(true);
});

test("normalized hotkey fallback is shown without replacing the focused recorder", async () => {
  const backend = page.backend;
  backend.override("ConfigSvc.Set", (value) => {
    backend.settings = structuredClone(value as Settings);
    backend.settings.hotkeys.panel = "Ctrl+Shift+A";
    backend.broadcastSettings();
  });
  await openSettings();
  await tab("快捷键");
  const recorder = select<HTMLButtonElement>("#rec-panel");
  recorder.focus();
  recorder.click();
  keydown(recorder, "k", { ctrlKey: true, altKey: true });
  await settle();
  expect(writes()[0]?.hotkeys.panel).toBe("Ctrl+Alt+K");
  expect(recorder.textContent?.trim()).toBe("Ctrl+Shift+A");
  expect(document.activeElement).toBe(recorder);
});

test("only one shortcut records and leaving the tab releases keyboard capture", async () => {
  await openSettings();
  await tab("快捷键");
  const panel = select<HTMLButtonElement>("#rec-panel");
  const selection = select<HTMLButtonElement>("#rec-selection");
  panel.click();
  selection.click();
  await tick();
  expect(panel.getAttribute("aria-pressed")).toBe("false");
  expect(selection.getAttribute("aria-pressed")).toBe("true");
  expect(keydown(selection, "ArrowUp")).toBe(false);
  await tab("翻译服务");
  expect(selection.getAttribute("aria-pressed")).toBe("false");
  const grip = select(".service-grip");
  grip.focus();
  keydown(grip, "ArrowDown");
  await settle();
  expect(writes()).toHaveLength(1);
  expect(page.backend.settings.services.order.slice(0, 2)).toEqual(["google", "index-translate"]);
});

test("hotkey checks cannot save an abandoned recording after unmount", async () => {
  const busy = deferred<boolean>();
  page.backend.override("HotkeySvc.Busy", () => busy.promise);
  await openSettings();
  await tab("快捷键");
  const recorder = select<HTMLButtonElement>("#rec-panel");
  recorder.click();
  keydown(recorder, " ", { code: "Space", ctrlKey: true });
  expect(page.backend.calls("HotkeySvc.Busy")).toEqual([["Ctrl+Space"]]);

  await page.destroy();
  busy.resolve(false);
  await settle();
  expect(writes()).toHaveLength(0);
  expect([...page.backend.events.values()].every((listeners) => listeners.size === 0)).toBe(true);
});

test("failed saves restore switches, language, naming and OpenAI options", async () => {
  const save = deferred<void>();
  let fail = true;
  const backend = page.backend;
  backend.override("ConfigSvc.Set", async (value) => {
    if (fail) {
      await save.promise;
      throw new Error("disk full");
    }
    backend.settings = structuredClone(value as Settings);
  });
  await openSettings();
  await expand("openai");
  const snake = 'input[aria-labelledby="copy-format-snake"]';
  const camel = 'input[aria-labelledby="copy-format-lowerCamel"]';
  change(openAtLogin, true);
  change("#auto-hide-switch", false);
  change(targetLanguage, "ja");
  change(autoSplit, false);
  change(snake, false);
  change("#openai-model", "unsaved-model");
  await tick();
  expect(select<HTMLInputElement>(camel).disabled).toBe(true);

  save.resolve();
  await settle();
  expect(select<HTMLInputElement>(openAtLogin).checked).toBe(false);
  expect(select<HTMLInputElement>("#auto-hide-switch").checked).toBe(true);
  expect(select<HTMLInputElement>(snake).checked).toBe(true);
  expect(select<HTMLInputElement>(camel).disabled).toBe(false);
  expect(select<HTMLSelectElement>(targetLanguage).value).toBe("zh-HANS");
  expect(select<HTMLInputElement>(autoSplit).checked).toBe(true);
  expect(select<HTMLInputElement>("#openai-model").value).toBe("gpt-4o-mini");
  expect(select(".toast-host").textContent).toContain("保存失败");

  fail = false;
  change(openAtLogin, true);
  await settle();
  expect(backend.settings.openAtLogin).toBe(true);
});

test("normalization restores OpenAI defaults without losing credential drafts or expansion", async () => {
  const backend = page.backend;
  backend.override("ConfigSvc.Set", (value) => {
    backend.settings = structuredClone(value as Settings);
    backend.settings.services.openai.baseURL ||= "https://api.openai.com/v1";
    backend.settings.services.openai.model ||= "gpt-4o-mini";
    backend.broadcastSettings();
  });
  await openSettings();
  await expand("openai");
  const key = draft("#input-openai-key", "unfinished-key");
  change("#openai-base-url", "");
  change("#openai-model", "");
  await settle();
  expect(select<HTMLInputElement>("#openai-base-url").value).toBe("https://api.openai.com/v1");
  expect(select<HTMLInputElement>("#openai-model").value).toBe("gpt-4o-mini");
  expect(key.value).toBe("unfinished-key");
  expect(select('[aria-controls="configure-openai"]').getAttribute("aria-expanded")).toBe("true");
});

test("a late readback and external settings update preserve an unsaved OpenAI draft", async () => {
  await openSettings();
  await expand("openai");
  const gate = deferred<Settings>();
  page.backend.override("ConfigSvc.Get", () => gate.promise);
  change(openAtLogin, true);
  await vi.waitFor(() => expect(page.backend.calls("ConfigSvc.Get")).toHaveLength(2));
  const model = draft("#openai-model", "still-typing");
  model.focus();
  const snapshot = structuredClone(page.backend.settings);
  snapshot.services.openai.model = "synced-model";
  gate.resolve(snapshot);
  await settle();
  expect(model.value).toBe("still-typing");
  expect(document.activeElement).toBe(model);
  page.backend.broadcastSettings();
  await tick();
  expect(model.value).toBe("still-typing");
  expect(writes()).toHaveLength(1);

  change("#openai-model", "still-typing");
  await settle();
  expect(page.backend.settings.services.openai.model).toBe("still-typing");
});

test("sequential saves keep each snapshot and the final combined state", async () => {
  const gate = deferred<void>();
  const backend = page.backend;
  const call = backend.call.getMockImplementation()!;
  let first = true;
  backend.override("ConfigSvc.Set", async (value) => {
    if (first) {
      first = false;
      await gate.promise;
    }
    await call("ConfigSvc.Set", value);
  });
  await openSettings();
  change(openAtLogin, true);
  await vi.waitFor(() => expect(writes()).toHaveLength(1));
  change(autoSplit, false);
  expect(writes()).toHaveLength(1);
  gate.resolve();
  await settle();
  expect(writes()).toHaveLength(2);
  expect(writes()[0]?.identifier.autoSplit).toBe(true);
  expect(writes()[1]?.openAtLogin).toBe(true);
  expect(backend.settings.identifier.autoSplit).toBe(false);
  expect(select<HTMLInputElement>(openAtLogin).checked).toBe(true);
  expect(select<HTMLInputElement>(autoSplit).checked).toBe(false);
});

test("a stale readback cannot erase a newer queued edit", async () => {
  await openSettings();
  const gate = deferred<Settings>();
  const call = page.backend.call.getMockImplementation()!;
  let next = true;
  page.backend.override("ConfigSvc.Get", () => {
    if (!next) return call("ConfigSvc.Get");
    next = false;
    return gate.promise;
  });
  change(openAtLogin, true);
  await vi.waitFor(() => expect(page.backend.calls("ConfigSvc.Get")).toHaveLength(2));
  const stale = structuredClone(page.backend.settings);
  change(targetLanguage, "ja");
  gate.resolve(stale);
  await settle();
  expect(page.backend.settings.targetLanguage).toBe("ja");
  expect(select<HTMLSelectElement>(targetLanguage).value).toBe("ja");
});

test("a stale readback cannot erase a newer settings event", async () => {
  await openSettings();
  const gate = deferred<Settings>();
  page.backend.override("ConfigSvc.Get", () => gate.promise);
  change(openAtLogin, true);
  await vi.waitFor(() => expect(page.backend.calls("ConfigSvc.Get")).toHaveLength(2));
  const stale = structuredClone(page.backend.settings);
  page.backend.settings.targetLanguage = "ja";
  page.backend.broadcastSettings();
  await tick();
  expect(select<HTMLSelectElement>(targetLanguage).value).toBe("ja");

  gate.resolve(stale);
  await settle();
  expect(select<HTMLSelectElement>(targetLanguage).value).toBe("ja");
});

test("external updates synchronize settings without interrupting shortcut recording", async () => {
  await openSettings();
  await expand("openai");
  await tab("快捷键");
  const recorder = select<HTMLButtonElement>("#rec-panel");
  recorder.click();
  page.backend.settings.openAtLogin = true;
  page.backend.settings.services.openai.model = "external-model";
  page.backend.broadcastSettings();
  await tick();
  expect(select<HTMLInputElement>(openAtLogin).checked).toBe(true);
  expect(select<HTMLInputElement>("#openai-model").value).toBe("external-model");
  expect(select("#rec-panel")).toBe(recorder);
  expect(recorder.textContent?.trim()).toBe("按下组合键…");
  keydown(recorder, "Escape");
  await tick();
  expect(recorder.textContent?.trim()).toBe(page.backend.settings.hotkeys.panel);
  expect(keydown(document, "ArrowUp")).toBe(true);
});

test.each(["settings", "keyring"] as const)("failed credential deletion (%s) refreshes status and permits retry", async (failure) => {
  await openSettings();
  await expand("baidu");
  const backend = page.backend;
  const call = backend.call.getMockImplementation()!;
  let fail = true;
  backend.override("ConfigSvc.SetSecret", (...args) => {
    if (!fail) return call("ConfigSvc.SetSecret", ...args);
    if (failure === "keyring") backend.disableBaidu();
    throw new Error(`${failure} delete failed`);
  });
  const clear = select<HTMLButtonElement>('[aria-label="清除APP ID"]');
  clear.click();
  await settle();
  const toggle = select<HTMLInputElement>(baiduToggle);
  expect(toggle.checked).toBe(failure === "settings");
  expect(toggle.disabled).toBe(false);
  expect(clear.disabled).toBe(false);
  expect(select(".service-footer-status").textContent).toContain("更新失败");

  fail = false;
  clear.click();
  await settle();
  expect(toggle.checked).toBe(false);
  expect(toggle.disabled).toBe(true);
  expect(page.target.contains(clear)).toBe(false);
});
