// @vitest-environment jsdom
import { beforeEach, expect, test, vi } from "vitest";
import { SHADOW_ITEM_MARKER_PROPERTY_NAME, SHADOW_PLACEHOLDER_ITEM_ID, SOURCES, TRIGGERS } from "svelte-dnd-action";
import { mount, tick } from "svelte";
import SettingsPage from "../src/components/SettingsPage.svelte";
import type { Settings } from "../src/mygo";
import { deferred, settle, setupPageTest } from "./support/page";

const page = setupPageTest();

// jsdom has no Web Animations API for Svelte's FLIP bookkeeping.
Object.defineProperty(Element.prototype, "getAnimations", {
  configurable: true,
  value: () => [],
});

beforeEach(() => {
  vi.stubGlobal("matchMedia", () => ({
    matches: true,
    addEventListener() {},
    removeEventListener() {},
  }));
});

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

function connectionTestButton(name: string): HTMLButtonElement {
  const button = [...select(`#configure-${name}`).querySelectorAll<HTMLButtonElement>("button")]
    .find((button) => button.textContent?.trim() === "测试连接");
  if (!button) throw new Error(`Missing connection test: ${name}`);
  return button;
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
const enabledServices = '.service-list[aria-label="已启用的翻译服务"]';

test("initial services tab and native requests select the service section", async () => {
  await openSettings();
  expect(select(".settings-section.active").id).toBe("section-services");
  expect(select(enabledServices).children).toHaveLength(3);

  await tab("通用");
  expect(select(".settings-section.active").id).toBe("section-general");
  page.backend.emit("open-translation-settings", true);
  await tick();
  expect(select(".settings-section.active").id).toBe("section-services");
});

test("automatic updates default to off and persist both switch states", async () => {
  await openSettings();
  await tab("通用");
  const selector = "#auto-update-switch";
  expect(select<HTMLInputElement>(selector).checked).toBe(false);
  change(selector, true);
  await settle();
  expect(page.backend.settings.autoUpdate).toBe(true);
  expect(select<HTMLInputElement>(selector).checked).toBe(true);
  change(selector, false);
  await settle();
  expect(page.backend.settings.autoUpdate).toBe(false);
  expect(select<HTMLInputElement>(selector).checked).toBe(false);
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

test("SiliconFlow and Zhipu save independent credentials and can both be enabled and tested", async () => {
  page.backend.settings.services.order.push("siliconflow", "zhipu");
  await openSettings();
  for (const [name, title, model] of [
    ["siliconflow", "硅基流动", "tencent/Hunyuan-MT-7B"],
    ["zhipu", "智谱", "glm-4.7-flash"],
  ] as const) {
    const toggle = `[aria-label="启用${title}"]`;
    expect(select<HTMLInputElement>(toggle).disabled).toBe(true);
    await expand(name);
    const drawer = select(`#configure-${name}`);
    expect(select<HTMLInputElement>(`#${name}-model`).value).toBe(model);
    draft(`#input-${name}-key`, ` ${name}-test-key `);
    await tick();
    expect(select<HTMLInputElement>(`#input-${name}-key`).type).toBe("password");
    expect([...drawer.querySelectorAll("button")].some((button) => ["显示", "隐藏"].includes(button.textContent?.trim() ?? ""))).toBe(false);
    drawer.querySelector<HTMLButtonElement>('button[type="submit"]')!.click();
    await vi.waitFor(() => expect(select<HTMLInputElement>(toggle).disabled).toBe(false));
    expect(select<HTMLInputElement>(`#input-${name}-key`).value).toBe("");
    expect(page.backend.secrets.get(`${name}-key`)).toBe(`${name}-test-key`);
    change(toggle, true);
    await settle();
    expect(select(enabledServices).querySelector(toggle)).not.toBeNull();
    connectionTestButton(name).click();
    await vi.waitFor(() => expect(select(`#configure-${name}`).textContent).toContain("连接成功"));
  }
  expect(page.backend.settings.services.enabled).toEqual(["index-translate", "google", "baidu", "siliconflow", "zhipu"]);
  expect(page.backend.calls("ConfigSvc.SetSecret")).toEqual([
    ["siliconflow-key", "siliconflow-test-key"],
    ["zhipu-key", "zhipu-test-key"],
  ]);
  expect(page.backend.calls("ConfigSvc.TestService")).toEqual([["siliconflow"], ["zhipu"]]);
  expect(JSON.stringify(writes())).not.toContain("test-key");
});

test("translation credentials remain masked and can still be cleared", async () => {
  await openSettings();
  await expand("baidu");
  await expand("openai");
  draft("#input-openai-key", "unfinished-secret");
  await tick();
  const fields = [...page.target.querySelectorAll<HTMLInputElement>(".credential-controls input")];
  expect(fields).toHaveLength(3);
  expect(fields.every((input) => input.type === "password")).toBe(true);
  const actions = [...page.target.querySelectorAll<HTMLButtonElement>(".credential-controls button")];
  expect(actions.map((button) => button.textContent?.trim())).toEqual(["清除", "清除"]);
  actions.find((button) => button.getAttribute("aria-label") === "清除密钥")!.click();
  await vi.waitFor(() => expect(page.backend.calls("ConfigSvc.SetSecret")).toEqual([["baidu-key", ""]]));
  await settle();
  expect(select<HTMLInputElement>("#input-openai-key").type).toBe("password");
  expect(select<HTMLInputElement>("#input-openai-key").value).toBe("unfinished-secret");
});

test.each([
  ["siliconflow", "tencent/Hunyuan-MT-7B", "Qwen/Qwen3-8B"],
  ["zhipu", "glm-4.7-flash", "glm-4.6"],
] as const)("%s saves its model independently and clearing restores the default", async (name, defaultModel, customModel) => {
  const backend = page.backend;
  backend.settings.services.order.push("siliconflow", "zhipu");
  backend.override("ConfigSvc.Set", (value) => {
    backend.settings = structuredClone(value as Settings);
    backend.settings.services.siliconflow.model ||= "tencent/Hunyuan-MT-7B";
    backend.settings.services.zhipu.model ||= "glm-4.7-flash";
    backend.broadcastSettings();
  });
  await openSettings();
  await expand(name);
  const selector = `#${name}-model`;
  expect(select<HTMLInputElement>(selector).value).toBe(defaultModel);
  draft(`#input-${name}-key`, "unfinished-key");
  draft(selector, ` ${customModel} `);
  change(selector, ` ${customModel} `);
  await settle();
  expect(backend.settings.services[name].model).toBe(customModel);
  expect(backend.settings.services.openai.model).toBe("gpt-4o-mini");
  const other = name === "zhipu" ? "siliconflow" : "zhipu";
  expect(backend.settings.services[other].model).toBe(other === "zhipu" ? "glm-4.7-flash" : "tencent/Hunyuan-MT-7B");

  draft(selector, " \t ");
  change(selector, " \t ");
  await settle();
  expect(backend.settings.services[name].model).toBe(defaultModel);
  expect(select<HTMLInputElement>(selector).value).toBe(defaultModel);
  expect(select<HTMLInputElement>(`#input-${name}-key`).value).toBe("unfinished-key");
  expect(select(`[aria-controls="configure-${name}"]`).getAttribute("aria-expanded")).toBe("true");
});

test.each(["openai", "siliconflow", "zhipu"] as const)("%s waits for its model save before probing and clears stale results", async (name) => {
  const backend = page.backend;
  if (!backend.settings.services.order.includes(name)) backend.settings.services.order.push(name);
  backend.secrets.set(`${name}-key`, "key");
  const saved = deferred<void>();
  const tested = deferred<string>();
  const call = backend.call.getMockImplementation()!;
  backend.override("ConfigSvc.Set", async (value) => {
    await saved.promise;
    await call("ConfigSvc.Set", value);
  });
  backend.override("ConfigSvc.TestService", (service) => {
    expect(service).toBe(name);
    expect(backend.settings.services[name].model).toBe("custom-model");
    return tested.promise;
  });
  await openSettings();
  await expand(name);
  change(`#${name}-model`, "custom-model");
  await tick();
  const drawer = select(`#configure-${name}`);
  const probe = connectionTestButton(name);
  probe.click();
  await settle();
  expect(backend.calls("ConfigSvc.TestService")).toHaveLength(0);
  expect(select<HTMLInputElement>(`#${name}-model`).disabled).toBe(true);
  if (name === "openai") expect(select<HTMLInputElement>("#openai-base-url").disabled).toBe(true);
  saved.resolve();
  await vi.waitFor(() => expect(backend.calls("ConfigSvc.TestService")).toEqual([[name]]));
  tested.resolve("");
  await settle();
  expect(drawer.textContent).toContain("连接成功");
  expect(select<HTMLInputElement>(`#${name}-model`).disabled).toBe(false);
  if (name === "openai") expect(select<HTMLInputElement>("#openai-base-url").disabled).toBe(false);
  draft(`#${name}-model`, "another-model");
  await tick();
  expect(drawer.textContent).not.toContain("连接成功");
});

test.each([
  ["openai", false], ["openai", true],
  ["siliconflow", false], ["siliconflow", true],
  ["zhipu", false], ["zhipu", true],
] as const)("%s aborts a probe after a failed save (readback fails: %s) and permits retry", async (name, readbackFails) => {
  const backend = page.backend;
  if (!backend.settings.services.order.includes(name)) backend.settings.services.order.push(name);
  backend.secrets.set(`${name}-key`, "key");
  await openSettings();
  await expand(name);
  const originalModel = backend.settings.services[name].model;
  const saved = deferred<void>();
  const call = backend.call.getMockImplementation()!;
  let fail = true;
  backend.override("ConfigSvc.Set", async (value) => {
    if (fail) {
      await saved.promise;
      throw new Error("disk full");
    }
    return call("ConfigSvc.Set", value);
  });
  backend.override("ConfigSvc.Get", () => {
    if (fail && readbackFails) throw new Error("read failed");
    return call("ConfigSvc.Get");
  });
  const testedModels: string[] = [];
  backend.override("ConfigSvc.TestService", () => {
    testedModels.push(backend.settings.services[name].model);
    return "";
  });

  change(`#${name}-model`, "unsaved-model");
  await tick();
  connectionTestButton(name).click();
  await settle();
  expect(backend.calls("ConfigSvc.TestService")).toHaveLength(0);
  saved.resolve();
  await vi.waitFor(() => expect(select(`#configure-${name}`).textContent).toContain("设置保存失败"));
  expect(backend.calls("ConfigSvc.TestService")).toHaveLength(0);
  expect(select(`#configure-${name}`).textContent).not.toContain("连接成功");
  expect(select<HTMLInputElement>(`#${name}-model`).value).toBe(readbackFails ? "unsaved-model" : originalModel);
  expect(select<HTMLInputElement>(`#${name}-model`).disabled).toBe(false);

  // A rolled-back write leaves the saved settings consistent, so a probe can
  // run again without editing anything; an unreconciled write still blocks it.
  connectionTestButton(name).click();
  await settle();
  expect(testedModels).toEqual(readbackFails ? [] : [originalModel]);

  fail = false;
  change(`#${name}-model`, "retry-model");
  await settle();
  connectionTestButton(name).click();
  await vi.waitFor(() => expect(select(`#configure-${name}`).textContent).toContain("连接成功"));
  expect(testedModels.at(-1)).toBe("retry-model");
  expect(backend.settings.services[name].model).toBe("retry-model");
});

test("a failed save does not block a queued save or a probe of the final settings", async () => {
  const backend = page.backend;
  backend.secrets.set("openai-key", "key");
  await openSettings();
  await expand("openai");
  const saved = deferred<void>();
  const call = backend.call.getMockImplementation()!;
  let first = true;
  backend.override("ConfigSvc.Set", async (value) => {
    if (first) {
      first = false;
      await saved.promise;
      throw new Error("disk full");
    }
    return call("ConfigSvc.Set", value);
  });
  backend.override("ConfigSvc.TestService", () => {
    expect(backend.settings.services.openai.model).toBe("queued-model");
    expect(backend.settings.identifier.autoSplit).toBe(false);
    return "";
  });
  change("#openai-model", "queued-model");
  change(autoSplit, false);
  await tick();
  connectionTestButton("openai").click();
  await settle();
  expect(backend.calls("ConfigSvc.Set")).toHaveLength(1);
  expect(backend.calls("ConfigSvc.TestService")).toHaveLength(0);
  saved.resolve();
  await vi.waitFor(() => expect(select("#configure-openai").textContent).toContain("连接成功"));
  expect(backend.calls("ConfigSvc.Set")).toHaveLength(2);
  expect(backend.calls("ConfigSvc.TestService")).toEqual([["openai"]]);
});

test("a save queued during a failed readback is not skipped", async () => {
  const backend = page.backend;
  backend.secrets.set("openai-key", "key");
  await openSettings();
  await expand("openai");
  const readback = deferred<Settings>();
  const call = backend.call.getMockImplementation()!;
  let blockRead = false;
  let blocked = false;
  let fail = true;
  backend.override("ConfigSvc.Get", () => {
    if (blockRead) {
      blockRead = false;
      blocked = true;
      return readback.promise;
    }
    return call("ConfigSvc.Get");
  });
  backend.override("ConfigSvc.Set", async (value) => {
    if (fail) {
      fail = false;
      throw new Error("disk full");
    }
    return call("ConfigSvc.Set", value);
  });
  backend.override("ConfigSvc.TestService", () => {
    expect(backend.settings.services.openai.model).toBe("failed-model");
    expect(backend.settings.openAtLogin).toBe(true);
    return "";
  });

  blockRead = true;
  change("#openai-model", "failed-model");
  await vi.waitFor(() => expect(blocked).toBe(true));
  // Queue a second save while the failed write waits for its rollback read.
  change(openAtLogin, true);
  await tick();
  readback.resolve(structuredClone(backend.settings));
  await tick();
  connectionTestButton("openai").click();
  await vi.waitFor(() => expect(select("#configure-openai").textContent).toContain("连接成功"));
  expect(backend.calls("ConfigSvc.TestService")).toHaveLength(1);
  expect(backend.settings.services.openai.model).toBe("failed-model");
  expect(backend.settings.openAtLogin).toBe(true);
});

test("dragging a service preserves its drawer, drafts and enablement", async () => {
  page.backend.secrets.set("openai-key", "key");
  page.backend.settings.services.enabled.push("openai");
  await openSettings();
  await expand("openai");
  draft("#input-openai-key", "unfinished-key");
  sortEvent("consider", TRIGGERS.DRAG_STARTED, ["index-translate", "google", "openai", "baidu"], "openai");
  await tick();
  sortEvent("finalize", TRIGGERS.DROPPED_INTO_ZONE, ["openai", "index-translate", "google", "baidu"], "openai");
  await settle();
  expect(page.backend.settings.services.order).toEqual(["openai", "index-translate", "google", "baidu", "deepl"]);
  expect(select('[aria-controls="configure-openai"]').getAttribute("aria-expanded")).toBe("true");
  expect(select<HTMLInputElement>("#input-openai-key").value).toBe("unfinished-key");
  expect(page.backend.settings.services.enabled).toEqual(["index-translate", "google", "baidu", "openai"]);
  expect(select(".service-order-status").textContent?.trim()).toBe("OpenAI 已移至第 1 位");
});

test("moving a service between groups preserves switch focus and updates its drag handle", async () => {
  await openSettings();
  const grip = '[aria-label^="排序百度翻译，"]';
  expect(select(enabledServices).contains(select(grip))).toBe(true);
  select(baiduToggle).focus();
  change(baiduToggle, false);
  await settle();
  expect(page.target.querySelector(grip)).toBeNull();
  expect(document.activeElement).toBe(select(baiduToggle));
  change(baiduToggle, true);
  await settle();
  const handle = select(grip);
  expect(select(enabledServices).contains(handle)).toBe(true);
  expect(document.activeElement).toBe(select(baiduToggle));
  expect(handle.title).toBe("拖动排序");
  const unclaimedPress = vi.fn();
  select("#section-services").addEventListener("mousedown", unclaimedPress);
  handle.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true }));
  window.dispatchEvent(new MouseEvent("mouseup"));
  expect(unclaimedPress).not.toHaveBeenCalled();
});

test("a failed service toggle restores the original switch state and focus", async () => {
  const save = deferred<void>();
  page.backend.override("ConfigSvc.Set", async () => {
    await save.promise;
    throw new Error("disk full");
  });
  await openSettings();
  select(baiduToggle).focus();
  change(baiduToggle, false);
  await settle();
  expect(select<HTMLInputElement>(baiduToggle).checked).toBe(false);
  expect(document.activeElement).toBe(select(baiduToggle));

  save.resolve();
  await settle();
  expect(select<HTMLInputElement>(baiduToggle).checked).toBe(true);
  expect(document.activeElement).toBe(select(baiduToggle));
});

function sortEvent(type: "consider" | "finalize", trigger: TRIGGERS, order: string[], id = "baidu"): void {
  const items = order.map((name) => name === id && type === "consider"
    ? { id: SHADOW_PLACEHOLDER_ITEM_ID, name, [SHADOW_ITEM_MARKER_PROPERTY_NAME]: true }
    : { id: name, name });
  select(enabledServices).dispatchEvent(new CustomEvent(type, {
    detail: { items, info: { trigger, id, source: SOURCES.POINTER } },
  }));
}

test("drag previews changes and saves only a completed drop in the enabled list", async () => {
  await openSettings();
  const original = ["index-translate", "google", "baidu"];
  const reordered = ["baidu", "index-translate", "google"];
  sortEvent("consider", TRIGGERS.DRAG_STARTED, original);
  sortEvent("consider", TRIGGERS.DRAGGED_OVER_INDEX, reordered);
  await tick();
  expect(select(enabledServices).firstElementChild?.querySelector(".service-title")?.textContent).toBe("百度翻译");
  expect(writes()).toHaveLength(0);

  sortEvent("finalize", TRIGGERS.DROPPED_OUTSIDE_OF_ANY, reordered);
  await settle();
  expect(select(enabledServices).firstElementChild?.querySelector(".service-title")?.textContent).toBe("Bilibili");
  expect(writes()).toHaveLength(0);

  sortEvent("consider", TRIGGERS.DRAG_STARTED, original);
  await tick();
  sortEvent("finalize", TRIGGERS.DROPPED_INTO_ZONE, reordered);
  await settle();
  expect(page.backend.settings.services.order).toEqual(["baidu", "index-translate", "google", "openai", "deepl"]);
  expect(page.backend.settings.services.enabled).toEqual(original);
  expect(writes()).toHaveLength(1);
});

test("switching tabs clears a pending drag and unmount releases settings listeners", async () => {
  await openSettings();
  sortEvent("consider", TRIGGERS.DRAG_STARTED, ["index-translate", "google", "baidu"]);
  await tick();
  const placeholder = ".service-row[data-is-dnd-shadow-item-hint]";
  expect(page.target.querySelector(placeholder)).not.toBeNull();

  await tab("通用");
  expect(page.target.querySelector(placeholder)).toBeNull();
  await tab("翻译服务");
  sortEvent("finalize", TRIGGERS.DROPPED_INTO_ZONE, ["baidu", "index-translate", "google"]);
  await settle();
  expect(writes()).toHaveLength(0);
  await page.destroy();
  expect([...page.backend.events.values()].every((listeners) => listeners.size === 0)).toBe(true);
});

test("a settings update cancels a drag when the enabled order changes", async () => {
  await openSettings();
  const original = ["index-translate", "google", "baidu"];
  const reordered = ["baidu", "index-translate", "google"];
  const placeholder = ".service-row[data-is-dnd-shadow-item-hint]";
  sortEvent("consider", TRIGGERS.DRAG_STARTED, original);
  sortEvent("consider", TRIGGERS.DRAGGED_OVER_INDEX, reordered);
  await tick();
  expect(page.target.querySelector(placeholder)).not.toBeNull();

  page.backend.settings.services.enabled = ["index-translate", "baidu"];
  page.backend.broadcastSettings();
  await tick();
  const googleToggle = '[aria-label="启用Google"]';
  expect(page.target.querySelectorAll(googleToggle)).toHaveLength(1);
  expect(select<HTMLInputElement>(googleToggle).checked).toBe(false);
  expect(page.target.querySelector(placeholder)).toBeNull();

  sortEvent("consider", TRIGGERS.DRAGGED_OVER_INDEX, reordered);
  await tick();
  expect(page.target.querySelector(placeholder)).toBeNull();
  sortEvent("finalize", TRIGGERS.DROPPED_INTO_ZONE, reordered);
  await settle();
  expect(writes()).toHaveLength(0);
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
  expect(keydown(document, "ArrowDown")).toBe(true);
  const grip = select(".service-grip");
  grip.focus();
  expect(keydown(grip, "ArrowDown")).toBe(true);
  await settle();
  expect(writes()).toHaveLength(0);
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

test("failed saves restore switches, language, naming and service model options", async () => {
  const save = deferred<void>();
  let fail = true;
  const backend = page.backend;
  backend.settings.services.order.push("siliconflow", "zhipu");
  backend.override("ConfigSvc.Set", async (value) => {
    if (fail) {
      await save.promise;
      throw new Error("disk full");
    }
    backend.settings = structuredClone(value as Settings);
  });
  await openSettings();
  await expand("openai");
  await expand("siliconflow");
  await expand("zhipu");
  const snake = 'input[aria-labelledby="copy-format-snake"]';
  const camel = 'input[aria-labelledby="copy-format-lowerCamel"]';
  change(openAtLogin, true);
  change("#auto-hide-switch", false);
  change(targetLanguage, "ja");
  change(autoSplit, false);
  change(snake, false);
  change("#openai-model", "unsaved-model");
  change("#siliconflow-model", "unsaved-siliconflow-model");
  change("#zhipu-model", "unsaved-zhipu-model");
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
  expect(select<HTMLInputElement>("#siliconflow-model").value).toBe("tencent/Hunyuan-MT-7B");
  expect(select<HTMLInputElement>("#zhipu-model").value).toBe("glm-4.7-flash");
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

test.each(["openai", "siliconflow", "zhipu"] as const)("a late readback and external settings update preserve an unsaved %s model draft", async (name) => {
  if (!page.backend.settings.services.order.includes(name)) page.backend.settings.services.order.push(name);
  await openSettings();
  await expand(name);
  const gate = deferred<Settings>();
  page.backend.override("ConfigSvc.Get", () => gate.promise);
  change(openAtLogin, true);
  await vi.waitFor(() => expect(page.backend.calls("ConfigSvc.Get")).toHaveLength(2));
  const model = draft(`#${name}-model`, "still-typing");
  model.focus();
  const snapshot = structuredClone(page.backend.settings);
  snapshot.services[name].model = "synced-model";
  gate.resolve(snapshot);
  await settle();
  expect(model.value).toBe("still-typing");
  expect(document.activeElement).toBe(model);
  page.backend.broadcastSettings();
  await tick();
  expect(model.value).toBe("still-typing");
  expect(writes()).toHaveLength(1);

  change(`#${name}-model`, "still-typing");
  await settle();
  expect(page.backend.settings.services[name].model).toBe("still-typing");
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
  const clear = '[aria-label="清除APP ID"]';
  select<HTMLButtonElement>(clear).click();
  await settle();
  expect(select<HTMLInputElement>(baiduToggle).checked).toBe(failure === "settings");
  expect(select<HTMLInputElement>(baiduToggle).disabled).toBe(false);
  expect(select<HTMLButtonElement>(clear).disabled).toBe(false);
  expect(select(".service-footer-status").textContent).toContain("更新失败");

  fail = false;
  select<HTMLButtonElement>(clear).click();
  await settle();
  expect(select<HTMLInputElement>(baiduToggle).checked).toBe(false);
  expect(select<HTMLInputElement>(baiduToggle).disabled).toBe(true);
  expect(page.target.querySelector(clear)).toBeNull();
});

test("OpenAI connection probe shows the standard success message", async () => {
  page.backend.secrets.set("openai-key", "key");
  await openSettings();
  await expand("openai");
  const drawer = select("#configure-openai");
  const probe = connectionTestButton("openai");
  probe.click();
  await settle();
  expect(page.backend.calls("ConfigSvc.TestService")).toEqual([["openai"]]);
  expect(drawer.querySelector('[role="status"]')?.textContent?.trim()).toBe("连接成功");
  draft("#openai-base-url", "https://example.com/v1");
  await tick();
  expect(drawer.querySelector('[role="status"]')?.textContent?.trim()).not.toBe("连接成功");
});
