import { ConfigSvc, HotkeySvc, events, type Settings } from "../mygo";
import { LANGUAGES, SERVICE_CREDENTIALS, SERVICE_KEY_PAGES, SERVICE_NAMES } from "../languages";
import { el, toast } from "../ui";

// The canonical copy-format list, in display order; labels match Go's
// naming.Labels.
const COPY_FORMATS = {
  lowerCamel: { label: "小驼峰", example: "userName" },
  upperCamel: { label: "大驼峰", example: "UserName" },
  snake: { label: "下划线", example: "user_name" },
  constant: { label: "常量", example: "USER_NAME" },
  kebab: { label: "短横线", example: "user-name" },
};

const SERVICE_ORDER = ["deepl", "openai", "baidu", "tencent", "youdao", "azure", "google"];

const NAV = [
  { id: "general", label: "通用" },
  { id: "services", label: "翻译服务" },
  { id: "hotkeys", label: "快捷键" },
  { id: "naming", label: "命名转换" },
] as const;

type SectionID = (typeof NAV)[number]["id"];

let settings: Settings;
let echoGuard = ""; // what we last saved, to ignore our own broadcast
let sectionHosts: Record<string, HTMLElement> = {};
let navButtons: Record<string, HTMLButtonElement> = {};

/** renderSettings mounts the settings page. */
export async function renderSettings(root: HTMLElement): Promise<void> {
  settings = await ConfigSvc.get();
  echoGuard = JSON.stringify(settings);

  const nav = el("nav", "settings-nav");
  for (const item of NAV) {
    const btn = el("button", undefined, item.label);
    btn.addEventListener("click", () => showSection(item.id));
    navButtons[item.id] = btn;
    nav.append(btn);
  }
  const main = el("main", "settings-main");
  const frame = el("div", "settings");
  frame.append(nav, main);
  root.append(frame);

  for (const item of NAV) {
    const host = el("section", "settings-section");
    host.id = `section-${item.id}`;
    sectionHosts[item.id] = host;
    main.append(host);
  }
  renderAll();
  showSection("general");

  events.settingsChanged.on((s: Settings) => {
    if (JSON.stringify(s) === echoGuard) return;
    settings = s;
    echoGuard = JSON.stringify(s);
    renderAll();
    showSection(currentSection);
  });
}

let currentSection: SectionID = "general";

function showSection(id: SectionID): void {
  currentSection = id;
  for (const item of NAV) {
    navButtons[item.id]!.classList.toggle("active", item.id === id);
    sectionHosts[item.id]!.classList.toggle("active", item.id === id);
  }
}

/** renderAll rebuilds every section from settings. */
function renderAll(): void {
  renderGeneral();
  renderServices();
  renderHotkeys();
  renderNaming();
}

/** save sends the whole settings; every change saves at once. */
async function save(): Promise<void> {
  echoGuard = JSON.stringify(settings);
  try {
    await ConfigSvc.set(settings);
  } catch (err) {
    toast(`保存失败：${String(err)}`);
  }
}

// ---- 通用 ----

function renderGeneral(): void {
  const host = sectionHosts.general!;
  host.replaceChildren(el("h2", undefined, "通用"));

  const target = el("select");
  const auto = el("option", undefined, "自动（中文↔英文）");
  auto.value = "auto";
  target.append(auto);
  for (const language of LANGUAGES) {
    const option = el("option", undefined, language.name);
    option.value = language.code;
    target.append(option);
  }
  target.value = settings.targetLanguage;
  target.addEventListener("change", () => {
    settings.targetLanguage = target.value;
    void save();
  });
  host.append(
    field("默认翻译到", "", target),
    switchField("开机自启", "登录后常驻托盘", settings.openAtLogin, (v) => {
      settings.openAtLogin = v;
      void save();
    }),
    switchField("失焦自动隐藏", "未钉住的气泡失去焦点即隐藏", settings.bubble.autoHide, (v) => {
      settings.bubble.autoHide = v;
      void save();
    }),
  );
}

function switchField(
  label: string,
  sub: string,
  checked: boolean,
  onChange: (v: boolean) => void,
): HTMLElement {
  const input = el("input");
  input.type = "checkbox";
  input.className = "switch";
  input.checked = checked;
  input.addEventListener("change", () => onChange(input.checked));
  return field(label, sub, input);
}

function field(label: string, sub: string, control: HTMLElement): HTMLElement {
  const row = el("div", "field");
  const lab = el("label", undefined, label);
  if (sub) lab.append(el("span", "sub", sub));
  row.append(lab, control);
  return row;
}

// ---- 翻译服务 ----

function renderServices(): void {
  const host = sectionHosts.services!;
  host.replaceChildren(el("h2", undefined, "翻译服务"));
  host.append(
    el("div", "hint-bar", "勾选的服务会同时翻译并在气泡中各显示一张卡片；上下移动调整卡片顺序。凭证保存在系统密钥环，不写入配置文件。"),
  );

  // Order the known services by the configured order, unknown ones last.
  const order = [...settings.services.order];
  for (const name of SERVICE_ORDER) {
    if (!order.includes(name)) order.push(name);
  }
  for (const name of order) {
    host.append(serviceRow(name));
  }
}

function serviceRow(name: string): HTMLElement {
  const row = el("div", "service-row");
  const enabled = settings.services.enabled.includes(name);

  const head = el("div", "field");
  const lab = el("label", undefined, SERVICE_NAMES[name] ?? name);
  if (name === "google") lab.append(el("span", "sub", "免密钥，尽力而为（国内网络可能需要代理）"));
  const right = el("div", "order-btns");
  if (SERVICE_KEY_PAGES.has(name)) {
    const keyPage = el("button", "text-btn", "获取密钥");
    keyPage.title = "打开官方控制台申请密钥";
    keyPage.addEventListener("click", () => {
      void ConfigSvc.openKeyPage(name).catch((err) => toast(String(err)));
    });
    right.append(keyPage);
  }
  const up = el("button", undefined, "↑");
  up.title = "上移";
  up.addEventListener("click", () => moveService(name, -1));
  const down = el("button", undefined, "↓");
  down.title = "下移";
  down.addEventListener("click", () => moveService(name, 1));
  const toggle = el("input");
  toggle.type = "checkbox";
  toggle.className = "switch";
  toggle.checked = enabled;
  toggle.addEventListener("change", () => {
    const has = settings.services.enabled.includes(name);
    settings.services.enabled = has
      ? settings.services.enabled.filter((s) => s !== name)
      : [...settings.services.enabled, name];
    void save();
  });
  right.append(up, down, toggle);
  head.append(lab, right);
  row.append(head);

  if (name === "openai") {
    row.append(openAIFields());
  }
  for (const cred of SERVICE_CREDENTIALS[name] ?? []) {
    row.append(credentialField(cred.name, cred.label, cred.placeholder));
  }
  if (name === "deepl") {
    row.append(deeplUsage());
  }
  return row;
}

function moveService(name: string, delta: number): void {
  const order = settings.services.order;
  const i = order.indexOf(name);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= order.length) return;
  [order[i], order[j]] = [order[j]!, order[i]!];
  void save();
  renderServices();
}

function openAIFields(): HTMLElement {
  const base = el("input");
  base.type = "text";
  base.value = settings.services.openai.baseURL;
  base.placeholder = "https://api.openai.com/v1";
  base.addEventListener("change", () => {
    settings.services.openai.baseURL = base.value.trim();
    void save();
  });
  const model = el("input");
  model.type = "text";
  model.value = settings.services.openai.model;
  model.placeholder = "gpt-4o-mini";
  model.addEventListener("change", () => {
    settings.services.openai.model = model.value.trim();
    void save();
  });
  const wrap = el("div");
  wrap.append(
    field("Base URL", "任何 OpenAI 兼容接口", base),
    field("模型", "", model),
  );
  return wrap;
}

function credentialField(name: string, label: string, placeholder: string): HTMLElement {
  const input = el("input");
  input.type = "password";
  input.placeholder = placeholder;
  const state = el("span", "usage-text");
  const refresh = () => {
    void ConfigSvc.hasSecret(name).then((has) => {
      state.textContent = has ? "已配置" : "未配置";
    });
  };
  refresh();
  input.addEventListener("change", async () => {
    if (!input.value) return;
    try {
      await ConfigSvc.setSecret(name, input.value);
      input.value = "";
      refresh();
      toast("凭证已保存");
    } catch (err) {
      toast(`保存失败：${String(err)}`);
    }
  });
  const clear = el("button", "text-btn", "清除");
  clear.addEventListener("click", async () => {
    await ConfigSvc.setSecret(name, "");
    refresh();
  });
  const controls = el("div");
  controls.append(input, state, clear);
  controls.style.display = "flex";
  controls.style.gap = "8px";
  controls.style.alignItems = "center";
  return field(label, "", controls);
}

function deeplUsage(): HTMLElement {
  const wrap = el("div");
  const bar = el("div", "usage-bar");
  bar.append(el("div"));
  const text = el("div", "usage-text", "额度查询中…");
  wrap.append(bar, text);
  void (async () => {
    try {
      if (!(await ConfigSvc.hasSecret("deepl-key"))) {
        text.textContent = "未配置 API Key";
        return;
      }
      const usage = await ConfigSvc.deeplUsage();
      const pct = usage.limit > 0 ? Math.min((usage.used / usage.limit) * 100, 100) : 0;
      (bar.firstElementChild as HTMLElement).style.width = `${pct}%`;
      text.textContent = `本月已用 ${usage.used.toLocaleString()} / ${usage.limit.toLocaleString()} 字符`;
    } catch {
      text.textContent = "额度查询失败";
    }
  })();
  return wrap;
}

// ---- 快捷键 ----

function renderHotkeys(): void {
  const host = sectionHosts.hotkeys!;
  host.replaceChildren(el("h2", undefined, "快捷键"));
  host.append(
    el("div", "hint-bar", "全局快捷键会覆盖其他应用中的相同组合。注册冲突时会自动尝试候选键位，并同步这里显示的当前生效值；点击输入框可更换。"),
  );
  const actions: { id: "panel" | "selection" | "ocr"; label: string }[] = [
    { id: "panel", label: "翻译面板" },
    { id: "selection", label: "划词翻译" },
    { id: "ocr", label: "截图翻译" },
  ];
  for (const action of actions) {
    host.append(recorderField(action.id, action.label));
  }
}

function recorderField(id: "panel" | "selection" | "ocr", label: string): HTMLElement {
  const input = el("input", "rec-input");
  input.type = "text";
  input.readOnly = true;
  input.value = settings.hotkeys[id];
  input.title = "点击后按下新组合键";
  const note = el("span", "conflict");
  const controls = el("div");
  controls.append(input);
  input.addEventListener("click", () => {
    note.textContent = "";
    input.classList.add("listening");
    input.value = "按下组合键…";

    const stop = () => {
      input.classList.remove("listening");
      input.value = settings.hotkeys[id];
      document.removeEventListener("keydown", onKey, true);
    };
    const onKey = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === "Escape") {
        stop();
        return;
      }
      const accel = toAccelerator(e);
      if (!accel) return; // a lone modifier, keep listening
      document.removeEventListener("keydown", onKey, true);
      input.classList.remove("listening");
      input.value = accel;
      void (async () => {
        if (accel !== settings.hotkeys[id] && (await HotkeySvc.busy(accel))) {
          note.textContent = "该组合键已被其他应用占用";
          input.value = settings.hotkeys[id];
          return;
        }
        settings.hotkeys[id] = accel;
        await save();
      })();
    };
    document.addEventListener("keydown", onKey, true);
  });
  const row = field(label, "", controls);
  const lab = row.querySelector("label");
  lab?.append(note);
  return row;
}

/** toAccelerator builds a mygo accelerator ("Alt+A") from a keydown;
 * null while only modifiers are held. */
function toAccelerator(e: KeyboardEvent): string | null {
  const mods: string[] = [];
  if (e.ctrlKey) mods.push("Ctrl");
  if (e.altKey) mods.push("Alt");
  if (e.shiftKey) mods.push("Shift");
  if (mods.length === 0) return null; // plain keys belong to the page
  let key = "";
  if (/^[a-zA-Z]$/.test(e.key)) key = e.key.toUpperCase();
  else if (/^F\d{1,2}$/.test(e.key)) key = e.key;
  else if (e.key === "Space") key = "Space";
  else if (/^[0-9]$/.test(e.key)) key = e.key;
  else return null;
  return [...mods, key].join("+");
}

// ---- 命名转换 ----

function renderNaming(): void {
  const host = sectionHosts.naming!;
  host.replaceChildren(el("h2", undefined, "命名转换"));

  host.append(
    switchField("自动拆分标识符", "英文输入自动识别 camelCase / snake_case / kebab-case 并拆词后翻译", settings.identifier.autoSplit, (v) => {
      settings.identifier.autoSplit = v;
      void save();
    }),
  );

  const group = el("fieldset", "copy-formats");
  const hint = el("p", "copy-formats-hint", "英语译文下方会显示已开启的复制格式。至少开启一种。");
  hint.id = "copy-formats-hint";
  group.setAttribute("aria-describedby", hint.id);
  const list = el("div", "copy-format-list");

  function updateDisabled(): void {
    for (const toggle of list.querySelectorAll("input")) {
      toggle.disabled = toggle.checked && settings.copyFormats.length === 1;
    }
  }

  for (const [style, { label, example }] of Object.entries(COPY_FORMATS)) {
    const row = el("label", "copy-format");
    const name = el("span", undefined, label);
    name.id = `copy-format-${style}`;
    const sample = el("code", undefined, example);
    sample.id = `${name.id}-example`;
    const toggle = el("input", "switch");
    toggle.type = "checkbox";
    toggle.checked = settings.copyFormats.includes(style);
    toggle.setAttribute("role", "switch");
    toggle.setAttribute("aria-labelledby", name.id);
    toggle.setAttribute("aria-describedby", sample.id);
    toggle.addEventListener("change", () => {
      settings.copyFormats = toggle.checked
        ? [...settings.copyFormats, style]
        : settings.copyFormats.filter((s) => s !== style);
      updateDisabled();
      void save();
    });
    row.append(name, sample, toggle);
    list.append(row);
  }
  updateDisabled();
  group.append(el("legend", undefined, "复制格式"), hint, list);
  host.append(group);
}
