import { beforeEach, expect, test } from "bun:test";
import { renderSettings } from "../src/pages/settings";
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
