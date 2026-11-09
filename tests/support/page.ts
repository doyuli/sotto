import { afterEach, beforeEach, vi } from "vitest";
import { tick, unmount, type mount } from "svelte";
import type { BubbleState, Settings } from "../../src/mygo";

class MockBackend {
  settings: Settings = {
    services: {
      enabled: ["index-translate", "google", "baidu"],
      order: ["index-translate", "google", "openai", "baidu", "deepl"],
      openai: { baseURL: "https://api.openai.com/v1", model: "gpt-4o-mini" },
      siliconflow: { model: "tencent/Hunyuan-MT-7B" },
      zhipu: { model: "glm-4.7-flash" },
    },
    targetLanguage: "zh-HANS",
    openAtLogin: false,
    autoUpdate: false,
    bubble: { autoHide: true },
    identifier: { autoSplit: true, stripCodeComments: false, flattenNewlines: false, stripMarkdown: false },
    copyFormats: ["lowerCamel", "snake"],
    hotkeys: { panel: "Ctrl+Alt+A", selection: "Ctrl+Alt+D", ocr: "Ctrl+Alt+S" },
  };

  bubble: BubbleState = {
    id: 1,
    maxHeight: 760,
    layoutID: 1,
    layoutSequence: 0,
    showID: 1,
    sessionID: "test-session",
    generation: 1,
    revision: 1,
    reqID: "initial",
    serviceRequests: {},
    text: "Hello world",
    sourceLang: "en",
    targetLang: "zh-HANS",
    trigger: "selection",
    phase: "",
    message: "",
    pinned: false,
    clipboardRecoveryID: "",
    meta: null,
    results: [],
    settings: {
      ...structuredClone(this.settings),
      services: { ...this.settings.services, enabled: ["google", "baidu"], order: ["google", "baidu"] },
    },
  };

  secrets = new Map([["baidu-appid", "app"], ["baidu-key", "secret"]]);
  events = new Map<string, Set<(payload: unknown) => void>>();

  on = (name: string, listener: (payload: unknown) => void) => {
    const listeners = this.events.get(name) ?? new Set();
    listeners.add(listener);
    this.events.set(name, listeners);
    return () => listeners.delete(listener);
  };

  call = vi.fn(async (method: string, ...args: unknown[]): Promise<unknown> => {
    switch (method) {
      case "ConfigSvc.Get":
        return structuredClone(this.settings);
      case "ConfigSvc.Credentials":
        return {
          configured: [
            "index-translate", "google",
            ...(this.secrets.has("openai-key") ? ["openai"] : []),
            ...(this.secrets.has("siliconflow-key") ? ["siliconflow"] : []),
            ...(this.secrets.has("zhipu-key") ? ["zhipu"] : []),
            ...(this.secrets.has("baidu-appid") && this.secrets.has("baidu-key") ? ["baidu"] : []),
          ],
          stored: [...this.secrets.keys()],
        };
      case "ConfigSvc.Set":
        this.settings = structuredClone(args[0] as Settings);
        this.broadcastSettings();
        return null;
      case "ConfigSvc.SetSecret": {
        const name = args[0] as string;
        const value = args[1] as string;
        if (value) this.secrets.set(name, value);
        else {
          if (name.startsWith("baidu-")) this.disableBaidu();
          this.secrets.delete(name);
        }
        return null;
      }
      case "HotkeySvc.Busy":
        return false;
      case "ConfigSvc.TestService":
        return "";
      case "BubbleSvc.Ready":
        return structuredClone(this.bubble);
      case "BubbleSvc.Rendered":
      case "BubbleSvc.Close":
        return true;
      case "TranslateSvc.Formats":
        return [{ style: "lowerCamel", label: "小驼峰", value: "userName" }];
      default:
        return null;
    }
  });

  emit(name: string, payload: unknown): void {
    for (const listener of this.events.get(name) ?? []) listener(payload);
  }

  broadcastSettings(): void {
    this.emit("settings-changed", structuredClone(this.settings));
  }

  disableBaidu(): void {
    this.settings.services.enabled = this.settings.services.enabled.filter((name) => name !== "baidu");
    this.broadcastSettings();
  }

  override(method: string, handler: (...args: unknown[]) => unknown): void {
    const fallback = this.call.getMockImplementation()!;
    this.call.mockImplementation(async (name, ...args) => name === method ? handler(...args) : fallback(name, ...args));
  }

  calls(method: string): unknown[][] {
    return this.call.mock.calls.filter(([name]) => name === method).map(([, ...args]) => args);
  }
}

export function setupPageTest() {
  let backend: MockBackend;
  let target: HTMLDivElement;
  let component: ReturnType<typeof mount> | undefined;

  beforeEach(() => {
    backend = new MockBackend();
    vi.stubGlobal("mygo", backend);
    target = document.createElement("div");
    document.body.append(target);
  });

  async function destroy(): Promise<void> {
    if (component) await unmount(component);
    component = undefined;
  }

  afterEach(async () => {
    await destroy();
    target.remove();
    history.replaceState(null, "", "/");
    sessionStorage.clear();
    vi.unstubAllGlobals();
  });

  return {
    get backend() { return backend; },
    get target() { return target; },
    track(instance: ReturnType<typeof mount>) { component = instance; },
    destroy,
  };
}

export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}

export async function settle(): Promise<void> {
  await new Promise<void>((resolve) => setTimeout(resolve, 0));
  await tick();
}
