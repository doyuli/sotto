import { ConfigSvc, HotkeySvc, events, type CredentialStatus, type Settings } from "../mygo";
import { LANGUAGES, SERVICE_CREDENTIALS, SERVICE_NAMES } from "../languages";
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

const NAV = [
  { id: "general", label: "通用" },
  { id: "services", label: "翻译服务" },
  { id: "hotkeys", label: "快捷键" },
  { id: "naming", label: "命名转换" },
] as const;

type SectionID = (typeof NAV)[number]["id"];

let settings: Settings;
let credentials: CredentialStatus;
let echoGuard = ""; // what we last saved, to ignore our own broadcast
let pendingSave = Promise.resolve();
let pendingCredentialWrite = Promise.resolve();
let savesInFlight = 0;
let sectionHosts: Record<string, HTMLElement> = {};
let navButtons: Record<string, HTMLButtonElement> = {};
const settingsRefreshers: (() => void)[] = [];

/** renderSettings mounts the settings page. */
export async function renderSettings(root: HTMLElement): Promise<void> {
  const params = new URLSearchParams(location.hash.split("?")[1] ?? "");
  let initialSection: SectionID = params.get("tab") === "services" ? "services" : "general";
  let mounted = false;
  events.openTranslationSettings.on(() => {
    if (mounted) showSection("services");
    else initialSection = "services";
  });
  [settings, credentials] = await Promise.all([ConfigSvc.get(), ConfigSvc.credentials()]);
  echoGuard = JSON.stringify(settings);
  settingsRefreshers.length = 0;

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
  renderServices();
  renderAll();
  showSection(initialSection);
  mounted = true;

  events.settingsChanged.on((s: Settings) => {
    if (savesInFlight || JSON.stringify(s) === echoGuard) return;
    settings = s;
    echoGuard = JSON.stringify(s);
    refreshSettings();
  });
}

function showSection(id: SectionID): void {
  for (const item of NAV) {
    navButtons[item.id]!.classList.toggle("active", item.id === id);
    sectionHosts[item.id]!.classList.toggle("active", item.id === id);
  }
}

/** Build controls once so settings updates preserve focus and drafts. */
function renderAll(): void {
  renderGeneral();
  renderHotkeys();
  renderNaming();
  refreshSettings();
}

function refreshSettings(): void {
  for (const refresh of settingsRefreshers) refresh();
  refreshServiceRows();
}

/** save sends the whole settings; every change saves at once. */
async function save(): Promise<void> {
  const snapshot = structuredClone(settings);
  savesInFlight++;
  pendingSave = pendingSave.then(async () => {
    try {
      await ConfigSvc.set(snapshot);
    } catch (err) {
      toast(`保存失败：${String(err)}`);
    } finally {
      savesInFlight--;
    }
    if (savesInFlight) return;
    const saved = await ConfigSvc.get();
    if (savesInFlight) return;
    settings = saved;
    echoGuard = JSON.stringify(saved);
    refreshSettings();
  }).catch((err) => toast(`读取设置失败：${String(err)}`));
  await pendingSave;
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
  settingsRefreshers.push(() => { target.value = settings.targetLanguage; });
  target.addEventListener("change", () => {
    settings.targetLanguage = target.value;
    void save();
  });
  host.append(
    field("默认翻译到", "", target),
    switchField("开机自启", "登录后常驻托盘", () => settings.openAtLogin, (v) => {
      settings.openAtLogin = v;
      void save();
    }),
    switchField("失焦自动隐藏", "未钉住的气泡失去焦点即隐藏", () => settings.bubble.autoHide, (v) => {
      settings.bubble.autoHide = v;
      void save();
    }),
  );
}

function switchField(
  label: string,
  sub: string,
  checked: () => boolean,
  onChange: (v: boolean) => void,
): HTMLElement {
  const input = el("input");
  input.type = "checkbox";
  input.className = "switch";
  input.checked = checked();
  settingsRefreshers.push(() => { input.checked = checked(); });
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

const serviceRows = new Map<string, { row: HTMLElement; refresh: () => void }>();
let serviceList: HTMLElement;

function renderServices(): void {
  const host = sectionHosts.services!;
  serviceList = el("div", "service-list");
  host.replaceChildren(
    el("h2", undefined, "翻译服务"),
    el("p", "hint-bar", "已启用的服务会同时翻译。凭证保存在系统密钥环。"),
    serviceList,
  );
  serviceRows.clear();
  for (const name of settings.services.order) {
    if (Object.hasOwn(SERVICE_CREDENTIALS, name)) serviceRows.set(name, serviceRow(name));
  }
}

function refreshServiceRows(): void {
  let next = serviceList.firstChild;
  for (const name of settings.services.order) {
    const service = serviceRows.get(name);
    if (!service) continue;
    if (service.row !== next) serviceList.insertBefore(service.row, next);
    next = service.row.nextSibling;
    service.refresh();
  }
}

function enabledServiceOrder(): string[] {
  return settings.services.order.filter(
    (name) => settings.services.enabled.includes(name) && credentials.configured.includes(name),
  );
}

function serviceRow(name: string): { row: HTMLElement; refresh: () => void } {
  const title = SERVICE_NAMES[name] ?? name;
  const fields = SERVICE_CREDENTIALS[name] ?? [];
  const keyless = fields.length === 0;
  let busy = false;
  let testing = false;
  const row = el("div", "service-row");
  const head = el("div", "field service-head");
  const label = el("div", "service-name", title);
  const state = el("span", "sub");
  state.setAttribute("aria-live", "polite");
  label.append(state);

  const actions = el("div", "order-btns");
  const up = orderButton("上移", "m6 15 6-6 6 6", -1);
  const down = orderButton("下移", "m6 9 6 6 6-6", 1);
  const toggle = el("input", "switch");
  toggle.type = "checkbox";
  toggle.setAttribute("aria-label", `启用${title}`);
  toggle.addEventListener("change", () => {
    const enabled = settings.services.enabled.filter((service) => service !== name);
    if (toggle.checked) enabled.push(name);
    settings.services.enabled = enabled;
    refreshServiceRows();
    void save();
  });
  const configure = el("button", "service-configure", "配置");
  const panel = el("div", "service-details");
  panel.id = `configure-${name}`;
  panel.hidden = true;
  configure.setAttribute("aria-controls", panel.id);
  configure.setAttribute("aria-expanded", "false");
  configure.setAttribute("aria-label", `配置${title}`);
  configure.addEventListener("click", () => {
    panel.hidden = !panel.hidden;
    configure.setAttribute("aria-expanded", String(!panel.hidden));
    configure.textContent = panel.hidden ? "配置" : "收起";
    configure.setAttribute("aria-label", panel.hidden ? `配置${title}` : `收起${title}配置`);
    if (!panel.hidden) refreshUsage();
  });
  if (!keyless) actions.append(configure);
  actions.append(up, down, toggle);
  head.append(label, actions);
  row.append(head);
  if (keyless) {
    row.append(el("p", "service-note", "免密钥 · 非官方接口，部分网络可能无法连接。"));
    return { row, refresh };
  }

  const hint = name === "baidu"
    ? "在百度翻译开放平台开通「通用文本翻译」，再填写控制台里的 APP ID 和密钥。"
    : name === "volcengine"
      ? "在火山引擎控制台开通「机器翻译」，并在访问控制中创建有调用权限的 Access Key。"
      : "";
  const getKey = el("button", "text-btn", "获取凭证");
  getKey.addEventListener("click", () => {
    void ConfigSvc.openKeyPage(name).catch((err) => toast(String(err)));
  });
  if (hint) panel.append(el("p", "service-note", hint));
  panel.append(getKey);
  if (name === "openai") panel.append(openAIFields());

  const form = el("form");
  const inputs = fields.map(({ name: key, label, placeholder }) => {
    const input = el("input");
    input.type = "password";
    input.placeholder = placeholder;
    input.autocomplete = "off";
    input.setAttribute("aria-label", label);
    const clear = el("button", "text-btn", "清除");
    clear.type = "button";
    clear.setAttribute("aria-label", `清除${label}`);
    clear.addEventListener("click", () => {
      void writeCredentials(async () => {
        await ConfigSvc.setSecret(key, "");
        input.value = "";
      }, "凭证已清除");
    });
    const controls = el("div", "credential-controls");
    controls.append(input, clear);
    const note = key === "azure-region" ? "可选，全局资源留空" : "";
    form.append(field(label, note, controls));
    return { key, input, clear, placeholder };
  });

  const saveKey = el("button", "service-configure", "保存凭证");
  saveKey.type = "submit";
  const test = el("button", "service-configure", "测试连接");
  test.type = "button";
  const result = el("span", "usage-text");
  result.setAttribute("role", "status");
  const usage = el("div");
  const footer = el("div", "service-footer");
  footer.append(saveKey, test, result);
  form.append(footer);
  panel.append(form, usage);
  row.append(panel);

  form.addEventListener("input", () => {
    result.textContent = "";
    refresh();
  });
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    if (busy || testing || saveKey.disabled) return;
    void writeCredentials(async () => {
      for (const { key, input } of inputs) {
        const value = input.value.trim();
        if (!value) continue;
        await ConfigSvc.setSecret(key, value);
        input.value = "";
      }
    }, "凭证已保存");
  });
  test.addEventListener("click", async () => {
    testing = true;
    result.textContent = "正在测试…";
    refresh();
    try {
      await pendingSave;
      const error = await ConfigSvc.testService(name);
      result.textContent = error || "连接成功";
    } catch (err) {
      result.textContent = `测试失败：${String(err)}`;
    } finally {
      testing = false;
      refresh();
    }
  });
  return { row, refresh };

  function refresh(): void {
    const configured = credentials.configured.includes(name);
    const enabled = settings.services.enabled.includes(name) && configured;
    state.textContent = enabled ? "已启用" : keyless ? "已关闭" : configured ? "已配置 · 已关闭" : "未配置";
    toggle.checked = enabled;
    toggle.disabled = !configured || busy;
    const order = enabledServiceOrder();
    const index = order.indexOf(name);
    up.hidden = down.hidden = !enabled || order.length < 2;
    up.disabled = index <= 0;
    down.disabled = index < 0 || index === order.length - 1;
    if (keyless) return;

    const hasDraft = inputs.some(({ input }) => input.value.trim() !== "");
    for (const { key, input, clear, placeholder } of inputs) {
      const stored = credentials.stored.includes(key);
      input.disabled = busy || testing;
      input.placeholder = stored ? "已保存，输入可替换" : placeholder;
      clear.hidden = !stored;
      clear.disabled = busy || testing;
    }
    saveKey.disabled = busy || testing || !hasDraft;
    test.disabled = busy || testing || !configured || hasDraft;
  }

  function orderButton(action: string, path: string, delta: number): HTMLButtonElement {
    const button = el("button", "service-order");
    button.title = action;
    button.setAttribute("aria-label", `${action}${title}`);
    button.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="${path}"/></svg>`;
    button.addEventListener("click", () => {
      const enabled = enabledServiceOrder();
      const neighbor = enabled[enabled.indexOf(name) + delta];
      if (!neighbor) return;
      const order = settings.services.order;
      const from = order.indexOf(name);
      const to = order.indexOf(neighbor);
      [order[from], order[to]] = [order[to]!, order[from]!];
      refreshServiceRows();
      (button.disabled ? (delta < 0 ? down : up) : button).focus();
      void save();
    });
    return button;
  }

  function writeCredentials(write: () => Promise<void>, success: string): void {
    busy = true;
    result.textContent = "";
    refresh();
    pendingCredentialWrite = pendingCredentialWrite.then(async () => {
      try {
        await pendingSave;
        await write();
        result.textContent = success;
      } catch (err) {
        result.textContent = `更新失败：${String(err)}`;
      }
      try {
        credentials = await ConfigSvc.credentials();
        refreshUsage();
      } catch (err) {
        toast(`读取凭证状态失败：${String(err)}`);
      } finally {
        busy = false;
        refreshServiceRows();
      }
    });
  }

  function refreshUsage(): void {
    if (name === "deepl" && !panel.hidden) {
      usage.replaceChildren(...(credentials.stored.includes("deepl-key") ? [deeplUsage()] : []));
    }
  }
}

function openAIFields(): HTMLElement {
  function option(key: "baseURL" | "model", placeholder: string): HTMLInputElement {
    const input = el("input");
    input.type = "text";
    input.value = settings.services.openai[key];
    input.placeholder = placeholder;
    let draft = false;
    input.addEventListener("input", () => { draft = input.value !== settings.services.openai[key]; });
    input.addEventListener("change", () => {
      draft = false;
      settings.services.openai[key] = input.value.trim();
      void save();
    });
    settingsRefreshers.push(() => {
      // A readback may finish while the user is typing the next edit.
      if (!draft) input.value = settings.services.openai[key];
    });
    return input;
  }
  const base = option("baseURL", "https://api.openai.com/v1");
  const model = option("model", "gpt-4o-mini");
  const wrap = el("div");
  wrap.append(
    field("Base URL", "任何 OpenAI 兼容接口", base),
    field("模型", "", model),
  );
  return wrap;
}

function deeplUsage(): HTMLElement {
  const wrap = el("div");
  const bar = el("div", "usage-bar");
  bar.append(el("div"));
  const text = el("div", "usage-text", "额度查询中…");
  wrap.append(bar, text);
  void (async () => {
    try {
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
  settingsRefreshers.push(() => {
    if (!input.classList.contains("listening")) input.value = settings.hotkeys[id];
  });
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
    switchField("自动拆分标识符", "英文输入自动识别 camelCase / snake_case / kebab-case 并拆词后翻译", () => settings.identifier.autoSplit, (v) => {
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
    settingsRefreshers.push(() => { toggle.checked = settings.copyFormats.includes(style); });
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
  settingsRefreshers.push(updateDisabled);
  group.append(el("legend", undefined, "复制格式"), hint, list);
  host.append(group);
}
