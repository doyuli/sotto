<script lang="ts">
  import { onMount } from "svelte";
  import {
    HotkeySvc,
    events,
    type HotkeySettings,
    type Settings,
  } from "../mygo";
  import { LANGUAGES } from "../languages";
  import { createToasts } from "../utils/toasts.svelte";
  import { createSettingsSync } from "../settings/settings-sync.svelte";
  import ServiceSettings from "./ServiceSettings.svelte";
  import ToastStack from "./ToastStack.svelte";

  const { messages, show: toast } = createToasts();

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

  const HOTKEYS = [
    { id: "panel", label: "翻译面板" },
    { id: "selection", label: "划词翻译" },
    { id: "ocr", label: "截图翻译" },
  ] as const satisfies readonly { id: keyof HotkeySettings; label: string }[];

  type SectionID = (typeof NAV)[number]["id"];

  let { initialTab = "general" }: { initialTab?: string } = $props();

  const sync = createSettingsSync(toast);
  const settings = $derived(sync.settings);
  const { save } = sync;
  let selectedTab = $state<SectionID>();
  const activeTab = $derived(selectedTab ?? (initialTab === "services" ? "services" : "general"));

  let hotkeyListening = $state<keyof HotkeySettings | null>(null);
  let hotkeyConflicts = $state<Record<string, string>>({});
  let hotkeyAttempt = 0;

  function setActiveTab(id: SectionID): void {
    if (id !== "hotkeys") cancelHotkey();
    selectedTab = id;
  }

  function changeServices(services: Settings["services"]): void {
    if (!settings) return;
    settings.services = services;
    void save();
  }

  onMount(() => {
    const stopOpening = events.openTranslationSettings.on(() => setActiveTab("services"));
    return () => {
      cancelHotkey();
      stopOpening();
    };
  });

  function startHotkeyRecording(id: keyof HotkeySettings): void {
    hotkeyAttempt++;
    hotkeyConflicts[id] = "";
    hotkeyListening = id;
  }

  function cancelHotkey(): void {
    hotkeyAttempt++;
    hotkeyListening = null;
  }

  async function handleHotkey(e: KeyboardEvent): Promise<void> {
    const id = hotkeyListening;
    if (!id || !settings) return;
    e.preventDefault();
    e.stopPropagation();
    if (e.key === "Escape") {
      cancelHotkey();
      return;
    }

    const mods: string[] = [];
    if (e.ctrlKey) mods.push("Ctrl");
    if (e.altKey) mods.push("Alt");
    if (e.shiftKey) mods.push("Shift");
    if (!mods.length) return;

    let key: string;
    if (/^[a-zA-Z]$/.test(e.key)) key = e.key.toUpperCase();
    else if (/^(F\d{1,2}|[0-9])$/.test(e.key)) key = e.key;
    else if (e.code === "Space" || e.key === " ") key = "Space";
    else return;

    const accel = [...mods, key].join("+");
    hotkeyListening = null;
    const attempt = ++hotkeyAttempt;
    try {
      const busy = accel !== settings.hotkeys[id] && await HotkeySvc.busy(accel);
      if (attempt !== hotkeyAttempt) return;
      if (busy) {
        hotkeyConflicts[id] = "该组合键已被其他应用占用";
        return;
      }
      settings.hotkeys[id] = accel;
      await save();
    } catch (err) {
      if (attempt === hotkeyAttempt) toast(String(err));
    }
  }
</script>

<svelte:document onkeydowncapture={handleHotkey} />

{#if settings}
  <div class="settings">
    <!-- Sidebar Navigation -->
    <nav class="settings-nav">
      {#each NAV as item (item.id)}
        <button
          type="button"
          class:active={activeTab === item.id}
          onclick={() => setActiveTab(item.id)}
        >
          {item.label}
        </button>
      {/each}

      <!-- Refined Security Pill (Single clean line) -->
      <div class="settings-security-pill" title="凭据由 Windows Credential Manager 本地加密保护">
        <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
        </svg>
        <span>Windows 凭据保护</span>
      </div>
    </nav>

    <!-- Main Content Area -->
    <main class="settings-main">
      <!-- 1. 通用 -->
      <section class="settings-section" class:active={activeTab === "general"} id="section-general">
        <h2>通用</h2>
        <div class="settings-card">
          <div class="field">
            <label for="target-language-select">
              默认翻译到
              <span class="sub">未指定目标时气泡首选的翻译语言</span>
            </label>
            <select
              id="target-language-select"
              value={settings.targetLanguage}
              onchange={(e) => {
                if (settings) {
                  settings.targetLanguage = e.currentTarget.value;
                  void save();
                }
              }}
            >
              <option value="auto">自动（中文↔英文）</option>
              {#each LANGUAGES as lang (lang.code)}
                <option value={lang.code}>{lang.name}</option>
              {/each}
            </select>
          </div>

          <div class="field">
            <label for="open-at-login-switch">
              开机自启
              <span class="sub">登录后常驻托盘</span>
            </label>
            <input
              id="open-at-login-switch"
              type="checkbox"
              class="switch"
              checked={settings.openAtLogin}
              onchange={(e) => {
                if (settings) {
                  settings.openAtLogin = e.currentTarget.checked;
                  void save();
                }
              }}
            />
          </div>

          <div class="field">
            <label for="auto-hide-switch">
              失焦自动隐藏
              <span class="sub">未钉住的气泡失去焦点即隐藏</span>
            </label>
            <input
              id="auto-hide-switch"
              type="checkbox"
              class="switch"
              checked={settings.bubble.autoHide}
              onchange={(e) => {
                if (settings) {
                  settings.bubble.autoHide = e.currentTarget.checked;
                  void save();
                }
              }}
            />
          </div>
        </div>
      </section>

      <ServiceSettings
        active={activeTab === "services"}
        services={settings.services}
        onServicesChange={changeServices}
        waitForSave={sync.waitForSave}
        {toast}
      />

      <!-- 3. 快捷键 -->
      <section class="settings-section" class:active={activeTab === "hotkeys"} id="section-hotkeys">
        <h2>快捷键</h2>
        <div class="hint-bar">
          全局快捷键会覆盖其他应用中的相同组合。注册冲突时会自动尝试候选键位，并同步这里显示的当前生效值；激活快捷键按钮可更换，按 Esc 取消。
        </div>

        <div class="settings-card">
          {#each HOTKEYS as action (action.id)}
            <div class="field">
              <label for={`rec-${action.id}`}>
                {action.label}
                {#if hotkeyConflicts[action.id]}
                  <span class="conflict">{hotkeyConflicts[action.id]}</span>
                {/if}
              </label>
              <div>
                <button
                  id={`rec-${action.id}`}
                  type="button"
                  class="rec-input"
                  class:listening={hotkeyListening === action.id}
                  aria-pressed={hotkeyListening === action.id}
                  title="激活后按下新组合键"
                  onclick={() => startHotkeyRecording(action.id)}
                >
                  {hotkeyListening === action.id ? "按下组合键…" : settings.hotkeys[action.id]}
                </button>
              </div>
            </div>
          {/each}
        </div>
      </section>

      <!-- 4. 命名转换 -->
      <section class="settings-section" class:active={activeTab === "naming"} id="section-naming">
        <h2>命名转换</h2>
        <div class="settings-card">
          <div class="field">
            <label for="auto-split-switch">
              自动拆分标识符
              <span class="sub">英文输入自动识别 camelCase / snake_case / kebab-case 并拆词后翻译</span>
            </label>
            <input
              id="auto-split-switch"
              type="checkbox"
              class="switch"
              checked={settings.identifier.autoSplit}
              onchange={(e) => {
                if (settings) {
                  settings.identifier.autoSplit = e.currentTarget.checked;
                  void save();
                }
              }}
            />
          </div>
        </div>

        <fieldset class="copy-formats" aria-describedby="copy-formats-hint">
          <legend>复制格式</legend>
          <p class="copy-formats-hint" id="copy-formats-hint">
            英语译文下方会显示已开启的复制格式。至少开启一种。
          </p>
          <div class="copy-format-list">
            {#each Object.entries(COPY_FORMATS) as [style, { label, example }] (style)}
              {@const isChecked = settings.copyFormats.includes(style)}
              {@const isOnlyOne = isChecked && settings.copyFormats.length === 1}
              <label class="copy-format">
                <span id={`copy-format-${style}`}>{label}</span>
                <code id={`copy-format-${style}-example`}>{example}</code>
                <input
                  type="checkbox"
                  class="switch"
                  role="switch"
                  aria-labelledby={`copy-format-${style}`}
                  aria-describedby={`copy-format-${style}-example`}
                  disabled={isOnlyOne}
                  checked={isChecked}
                  onchange={(e) => {
                    if (settings) {
                      settings.copyFormats = e.currentTarget.checked
                        ? [...settings.copyFormats, style]
                        : settings.copyFormats.filter((s) => s !== style);
                      void save();
                    }
                  }}
                />
              </label>
            {/each}
          </div>
        </fieldset>
      </section>
    </main>
  </div>
{/if}

<ToastStack {messages} />
