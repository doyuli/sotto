import { beforeEach, expect, test } from "bun:test";
import { disposeSettings, renderSettings } from "../src/pages/settings";
import type { Settings } from "../src/mygo";
import { Element, TestDocument } from "./dom";

const clone = <T>(value: T): T => structuredClone(value);
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => { resolve = done; });
  return { promise, resolve };
}

class Backend {
  settings: Settings = {
    services: {
      enabled: ["index-translate", "google", "baidu"], order: ["index-translate", "google", "openai", "baidu"],
      openai: { baseURL: "https://api.openai.com/v1", model: "gpt-4o-mini" },
    },
    targetLanguage: "zh-HANS", openAtLogin: false, bubble: { autoHide: true },
    identifier: { autoSplit: true }, copyFormats: ["snake", "constant"],
    hotkeys: { panel: "Ctrl+Alt+A", selection: "Ctrl+Alt+D", ocr: "Ctrl+Alt+S" },
  };
  secrets = new Map([["baidu-appid", "app"], ["baidu-key", "secret"]]);
  events = new Map<string, Set<(payload: unknown) => void>>();
  writes: Settings[] = [];
  reads = 0;
  rejectSave = false;
  rejectSecret: "settings" | "keyring" | null = null;
  nextSet: Promise<void> | null = null;
  nextGet: Promise<void> | null = null;
  tests: string[] = [];

  on = (name: string, listener: (payload: unknown) => void) => {
    if (!this.events.has(name)) this.events.set(name, new Set());
    this.events.get(name)!.add(listener);
    return () => this.events.get(name)!.delete(listener);
  };

  broadcast() {
    for (const listener of this.events.get("settings-changed") ?? []) listener(clone(this.settings));
  }

  call = async (method: string, ...args: any[]) => {
    switch (method) {
      case "ConfigSvc.Get": {
        const result = clone(this.settings);
        const gate = this.nextGet;
        this.nextGet = null;
        if (gate) await gate;
        this.reads++;
        return result;
      }
      case "ConfigSvc.Credentials":
        return {
          stored: [...this.secrets.keys()],
          configured: ["index-translate", "google", ...(this.secrets.has("openai-key") ? ["openai"] : []),
            ...(this.secrets.has("baidu-appid") && this.secrets.has("baidu-key") ? ["baidu"] : [])],
        };
      case "ConfigSvc.Set": {
        const next: Settings = clone(args[0]);
        this.writes.push(clone(next));
        const gate = this.nextSet;
        this.nextSet = null;
        if (gate) await gate;
        if (this.rejectSave) throw new Error("disk full");
        if (next.hotkeys.panel === "Ctrl+Alt+K") next.hotkeys.panel = "Ctrl+Shift+A";
        next.services.openai.baseURL ||= "https://api.openai.com/v1";
        next.services.openai.model ||= "gpt-4o-mini";
        this.settings = next;
        this.broadcast();
        return;
      }
      case "ConfigSvc.SetSecret": {
        const [name, value] = args;
        if (this.rejectSecret === "settings") throw new Error("settings write failed");
        // Go disables a service before deleting its required credential.
        if (!value && name.startsWith("baidu-")) {
          this.settings.services.enabled = this.settings.services.enabled.filter((name) => name !== "baidu");
          this.broadcast();
        }
        if (this.rejectSecret === "keyring") throw new Error("keyring delete failed");
        if (value) this.secrets.set(name, value);
        else this.secrets.delete(name);
        return;
      }
      case "ConfigSvc.TestService": this.tests.push(args[0]); return "";
      case "HotkeySvc.Busy": return false;
      default: throw new Error(`Unexpected call: ${method}`);
    }
  };
}

let doc: TestDocument;
let root: Element;
let backend: Backend;
beforeEach(async () => {
  doc = new TestDocument();
  root = doc.createElement("div");
  doc.body.append(root);
  backend = new Backend();
  Object.assign(globalThis, { document: doc, location: { hash: "#/settings" }, mygo: backend });
  await renderSettings(root as unknown as HTMLElement);
});

function field(label: string): Element {
  const row = root.querySelectorAll(".field").find((row) => row.querySelector("label")?.textContent === label);
  if (!row) throw new Error(`Missing field: ${label}`);
  return row.querySelector("input") ?? row.querySelector("select")!;
}
function aria(label: string): Element {
  const node = [...root.querySelectorAll("input"), ...root.querySelectorAll("button")]
    .find((node) => node.attributes.get("aria-label") === label);
  if (!node) throw new Error(`Missing control: ${label}`);
  return node;
}
function change(input: Element, value: string | boolean) {
  if (typeof value === "boolean") input.checked = value;
  else input.value = value;
  input.emit("change");
}
function record(input: Element) {
  input.emit("click");
  doc.emit("keydown", { key: "k", ctrlKey: true, altKey: true });
}

test("Index-Translate can be turned off independently of OpenAI", async () => {
  const index = aria("启用Bilibili");
  const openai = aria("启用OpenAI");
  expect(index.checked).toBe(true);
  expect(index.disabled).toBe(false);
  expect(openai.disabled).toBe(true);
  change(index, false);
  await tick();
  expect(backend.settings.services.enabled).toEqual(["google", "baidu"]);
  expect(openai.disabled).toBe(true);
});

test("unconfigured and disabled services can be reordered without changing enablement", async () => {
  change(aria("启用百度翻译"), false);
  await tick();
  aria("配置OpenAI").emit("click");
  const draft = aria("API Key");
  draft.value = "unfinished-key";
  draft.emit("input");

  const top = aria("置顶OpenAI");
  expect(top.disabled).toBe(false);
  top.emit("click");
  await tick();
  expect(backend.settings.services.order).toEqual(["openai", "index-translate", "google", "baidu"]);
  expect(top.disabled).toBe(true);
  expect(aria("启用OpenAI").disabled).toBe(true);
  expect(aria("收起OpenAI配置").attributes.get("aria-expanded")).toBe("true");
  expect(draft.value).toBe("unfinished-key");

  aria("置顶百度翻译").emit("click");
  await tick();
  expect(backend.settings.services.order).toEqual(["baidu", "openai", "index-translate", "google"]);
  const grip = root.querySelector(".service-grip")!;
  expect(doc.activeElement).toBe(grip);
  grip.emit("keydown", { key: "ArrowDown" });
  await tick();
  expect(backend.settings.services.order).toEqual(["openai", "baidu", "index-translate", "google"]);
  expect(backend.settings.services.enabled).toEqual(["index-translate", "google"]);
  expect(doc.activeElement).toBe(grip);
});

test("dropping a service at either end saves its order without changing enablement", async () => {
  const list = root.querySelector(".service-list")!;
  const rows = list.querySelectorAll(".service-row");
  const moved = rows[2]!;
  const grip = moved.querySelector(".service-grip")!;
  for (const row of rows) {
    const rect = () => ({ left: 0, top: list.children.indexOf(row) * 90, height: 80 });
    Object.assign(row, { getBoundingClientRect: rect });
    Object.assign(row.querySelector(".service-head")!, { getBoundingClientRect: rect });
  }
  const dataTransfer = { setData() {}, setDragImage() {} };
  function dropAt(clientY: number) {
    grip.emit("dragstart", { dataTransfer, clientX: 10, clientY: 10 });
    list.emit("dragover", { dataTransfer, clientY });
    list.emit("drop", { dataTransfer, clientY });
  }

  dropAt(-5);
  await tick();
  expect(list.firstChild).toBe(moved);
  expect(backend.settings.services.order).toEqual(["openai", "index-translate", "google", "baidu"]);
  expect(moved.classList.contains("dragging")).toBe(false);
  expect(rows[0]!.attributes.has("data-drop")).toBe(false);

  dropAt(-5);
  await tick();
  expect(backend.writes).toHaveLength(1);

  dropAt(355);
  await tick();
  expect(list.children.at(-1)).toBe(moved);
  expect(backend.settings.services.order).toEqual(["index-translate", "google", "baidu", "openai"]);
  expect(backend.settings.services.enabled).toEqual(["index-translate", "google", "baidu"]);
  expect(backend.writes).toHaveLength(2);
  expect(rows[3]!.attributes.has("data-drop")).toBe(false);
});

test("unmounting settings cancels an active drag and releases its listeners", async () => {
  const list = root.querySelector(".service-list")!;
  const [first, second] = list.querySelectorAll(".service-row");
  const grip = second!.querySelector(".service-grip")!;
  Object.assign(second!.querySelector(".service-head")!, {
    getBoundingClientRect: () => ({ left: 0, top: 80 }),
  });
  Object.assign(first!, { getBoundingClientRect: () => ({ top: 0, height: 80 }) });
  const dataTransfer = { setData() {}, setDragImage() {} };
  grip.emit("dragstart", { dataTransfer, clientX: 10, clientY: 100 });
  list.emit("dragover", { dataTransfer, clientY: 10 });
  expect(second!.classList.contains("dragging")).toBe(true);
  expect(first!.attributes.get("data-drop")).toBe("before");

  field("翻译面板").emit("click");
  disposeSettings();
  expect(second!.classList.contains("dragging")).toBe(false);
  expect(first!.attributes.has("data-drop")).toBe(false);
  expect(grip.listeners.get("dragstart")?.size).toBe(0);
  expect(grip.listeners.get("dragend")?.size).toBe(0);
  expect(list.listeners.get("dragover")?.size).toBe(0);
  expect(list.listeners.get("dragleave")?.size).toBe(0);
  expect(list.listeners.get("drop")?.size).toBe(0);
  expect(doc.listeners.get("keydown")?.size).toBe(0);
  expect([...backend.events.values()].every((listeners) => listeners.size === 0)).toBe(true);

  root.replaceChildren();
  await renderSettings(root as unknown as HTMLElement);
  expect([...backend.events.values()].map((listeners) => listeners.size)).toEqual([1, 1]);
  expect(backend.writes).toHaveLength(0);
});

test("late settings reads cannot mount a page after disposal", async () => {
  const gate = deferred();
  backend.nextGet = gate.promise;
  root.replaceChildren();
  const mounting = renderSettings(root as unknown as HTMLElement);
  await tick();
  disposeSettings();
  gate.resolve();
  await mounting;
  expect(root.children).toHaveLength(0);
  expect([...backend.events.values()].every((listeners) => listeners.size === 0)).toBe(true);
});

test("hotkey fallback is shown without replacing the focused control", async () => {
  const input = field("翻译面板");
  input.focus();
  record(input);
  await tick();
  expect(backend.settings.hotkeys.panel).toBe("Ctrl+Shift+A");
  expect(input.value).toBe("Ctrl+Shift+A");
  expect(field("翻译面板")).toBe(input);
  expect(doc.activeElement).toBe(input);
});

test("only one shortcut records at a time and leaving the tab releases arrow keys", async () => {
  const nav = root.querySelector(".settings-nav")!.querySelectorAll("button");
  nav.find((button) => button.textContent === "快捷键")!.emit("click");
  const panel = field("翻译面板");
  const selection = field("划词翻译");
  panel.emit("click");
  panel.emit("click");
  selection.emit("click");
  expect(doc.listeners.get("keydown")?.size).toBe(1);
  expect(panel.value).toBe("Ctrl+Alt+A");
  expect(selection.classList.contains("listening")).toBe(true);

  nav.find((button) => button.textContent === "翻译服务")!.emit("click");
  expect(doc.listeners.get("keydown")?.size).toBe(0);
  expect(selection.value).toBe("Ctrl+Alt+D");
  root.querySelector(".service-grip")!.emit("keydown", { key: "ArrowDown" });
  await tick();
  expect(backend.settings.services.order).toEqual(["google", "index-translate", "openai", "baidu"]);
  expect(backend.writes).toHaveLength(1);
});

test("failed saves restore switches, language, naming and OpenAI options", async () => {
  backend.rejectSave = true;
  change(field("开机自启"), true);
  change(field("失焦自动隐藏"), false);
  change(field("默认翻译到"), "en");
  change(field("自动拆分标识符"), false);
  const format = root.querySelectorAll("input").find((input) => input.attributes.get("aria-labelledby") === "copy-format-snake")!;
  change(format, false);
  change(field("模型"), "unsaved-model");
  await tick();
  expect(field("开机自启").checked).toBe(false);
  expect(field("失焦自动隐藏").checked).toBe(true);
  expect(field("默认翻译到").value).toBe("zh-HANS");
  expect(field("自动拆分标识符").checked).toBe(true);
  expect(format.checked).toBe(true);
  expect(format.disabled).toBe(false);
  expect(field("模型").value).toBe("gpt-4o-mini");
  backend.rejectSave = false;
  change(field("开机自启"), true);
  await tick();
  expect(backend.settings.openAtLogin).toBe(true);
});

test("normalization updates OpenAI fields and preserves credential drafts and expansion", async () => {
  aria("配置OpenAI").emit("click");
  const key = aria("API Key");
  key.value = "unfinished-key";
  key.emit("input");
  change(field("Base URL"), "");
  change(field("模型"), "");
  await tick();
  expect(field("Base URL").value).toBe("https://api.openai.com/v1");
  expect(field("模型").value).toBe("gpt-4o-mini");
  expect(key.value).toBe("unfinished-key");
  expect(aria("收起OpenAI配置").attributes.get("aria-expanded")).toBe("true");
});

test("a late readback preserves a new OpenAI draft", async () => {
  const gate = deferred();
  backend.nextGet = gate.promise;
  change(field("开机自启"), true);
  await tick();
  const model = field("模型");
  model.focus();
  model.value = "still-typing";
  model.emit("input");
  gate.resolve();
  await tick();
  expect(model.value).toBe("still-typing");
  expect(doc.activeElement).toBe(model);
  change(model, "still-typing");
  await tick();
  expect(backend.settings.services.openai.model).toBe("still-typing");
});

test("sequential saves keep each snapshot and final combined state", async () => {
  const gate = deferred();
  backend.nextSet = gate.promise;
  change(field("开机自启"), true);
  await tick();
  change(field("自动拆分标识符"), false);
  const writesBeforeRelease = backend.writes.length;
  gate.resolve();
  await tick();
  expect(writesBeforeRelease).toBe(1);
  expect(backend.writes).toHaveLength(2);
  expect(backend.writes[0]!.identifier.autoSplit).toBe(true);
  expect(backend.writes[1]!.openAtLogin).toBe(true);
  expect(backend.settings.identifier.autoSplit).toBe(false);
  expect(field("开机自启").checked).toBe(true);
  expect(field("自动拆分标识符").checked).toBe(false);
});

test("a stale readback cannot erase a newer queued edit", async () => {
  const gate = deferred();
  backend.nextGet = gate.promise;
  change(field("开机自启"), true);
  await tick();
  change(field("默认翻译到"), "ja");
  gate.resolve();
  await tick();
  expect(backend.settings.openAtLogin).toBe(true);
  expect(backend.settings.targetLanguage).toBe("ja");
  expect(field("默认翻译到").value).toBe("ja");
});

test("external updates synchronize fields without interrupting shortcut recording", async () => {
  const recorder = field("翻译面板");
  recorder.emit("click");
  backend.settings.openAtLogin = true;
  backend.settings.services.openai.model = "external-model";
  backend.broadcast();
  expect(field("开机自启").checked).toBe(true);
  expect(field("模型").value).toBe("external-model");
  expect(field("翻译面板")).toBe(recorder);
  expect(recorder.value).toBe("按下组合键…");
  doc.emit("keydown", { key: "Escape" });
  expect(recorder.value).toBe(backend.settings.hotkeys.panel);
  expect(doc.listeners.get("keydown")?.size).toBe(0);
});

for (const failure of ["settings", "keyring"] as const) {
  test(`failed credential deletion (${failure}) refreshes status and permits retry`, async () => {
    backend.rejectSecret = failure;
    aria("清除APP ID").emit("click");
    await tick();
    const toggle = aria("启用百度翻译");
    expect(toggle.checked).toBe(failure === "settings");
    expect(toggle.disabled).toBe(false);
    expect(aria("清除APP ID").hidden).toBe(false);
    expect(aria("清除APP ID").disabled).toBe(false);
    expect(root.querySelectorAll("span").some((node) => node.textContent.startsWith("更新失败："))).toBe(true);
    backend.rejectSecret = null;
    aria("清除APP ID").emit("click");
    await tick();
    expect(toggle.checked).toBe(false);
    expect(toggle.disabled).toBe(true);
    expect(aria("清除APP ID").hidden).toBe(true);
  });
}
