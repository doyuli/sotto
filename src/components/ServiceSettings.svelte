<script lang="ts">
  import { onMount, tick } from "svelte";
  import { ConfigSvc, type CredentialStatus, type OpenAIOptions, type Settings } from "../mygo";
  import { SERVICE_CREDENTIALS, SERVICE_NAMES, SERVICE_NOTES } from "../languages";

  let { active, services, onServicesChange, waitForSave, toast }: {
    active: boolean;
    services: Settings["services"];
    onServicesChange: (services: Settings["services"]) => void;
    waitForSave: () => Promise<void>;
    toast: (message: string) => void;
  } = $props();

  let credentials = $state<CredentialStatus | null>(null);
  let openAIDrafts = $state<Partial<OpenAIOptions>>({});

  let expandedServices = $state<Record<string, boolean>>({});
  let serviceDrafts = $state<Record<string, Record<string, string>>>({});
  let showSecrets = $state<Record<string, boolean>>({});
  let credentialBusy = $state<Record<string, boolean>>({});
  let testStatuses = $state<Record<string, { testing: boolean; result: string; isError?: boolean }>>({});
  let deeplUsageText = $state<string>("额度查询中…");
  let deeplUsagePct = $state<number>(0);

  let serviceOrderStatus = $state<string>("");
  let draggedService = $state<string | null>(null);
  let dropTarget = $state<{ name: string; after: boolean } | null>(null);

  let pendingCredentialWrite = Promise.resolve();
  let disposed = false;

  async function refreshCredentials(): Promise<void> {
    if (disposed) return;
    try {
      const status = await ConfigSvc.credentials();
      if (!disposed) credentials = status;
    } catch (err) {
      if (!disposed) toast(`读取凭证状态失败：${String(err)}`);
    }
  }

  onMount(() => {
    void refreshCredentials();
    return () => { disposed = true; };
  });

  $effect(() => {
    if (!active) endServiceDrag();
  });

  // Services reordering logic
  function moveService(name: string, target: string, after = false): void {
    const order = [...services.order];
    const from = order.indexOf(name);
    const targetIndex = order.indexOf(target);
    if (from < 0 || targetIndex < 0) return;
    const insertion = targetIndex + Number(after);
    const to = from < insertion ? insertion - 1 : insertion;
    if (from === to) return;
    order.splice(from, 1);
    order.splice(to, 0, name);
    const position = order.slice(0, to + 1).filter((s) => Object.hasOwn(SERVICE_CREDENTIALS, s)).length;
    serviceOrderStatus = `${SERVICE_NAMES[name] ?? name} 已移至第 ${position} 位`;
    onServicesChange({ ...services, order });
  }

  function endServiceDrag(): void {
    draggedService = null;
    dropTarget = null;
  }

  function onGripDragStart(e: DragEvent, name: string): void {
    if (!e.dataTransfer) return;
    e.dataTransfer.setData("application/x-sotto-service", name);
    e.dataTransfer.effectAllowed = "move";
    draggedService = name;
  }

  function onRowDragOver(e: DragEvent, name: string): void {
    if (!draggedService || draggedService === name || !e.dataTransfer) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    const targetElem = e.currentTarget as HTMLElement;
    const rect = targetElem.getBoundingClientRect();
    const after = e.clientY > rect.top + rect.height / 2;
    dropTarget = { name, after };
  }

  function onRowDrop(e: DragEvent, name: string): void {
    e.preventDefault();
    if (!draggedService) return;
    const target = dropTarget;
    const source = draggedService;
    endServiceDrag();
    if (target?.name === name) moveService(source, name, target.after);
  }

  function onGripKeydown(e: KeyboardEvent, name: string): void {
    if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
    e.preventDefault();
    const order = services.order.filter((s) => Object.hasOwn(SERVICE_CREDENTIALS, s));
    const down = e.key === "ArrowDown";
    const target = order[order.indexOf(name) + (down ? 1 : -1)];
    if (!target) return;
    const grip = e.currentTarget as HTMLElement;
    moveService(name, target, down);
    void tick().then(() => { if (active && grip.isConnected) grip.focus(); });
  }

  function moveToTop(e: MouseEvent, name: string): void {
    const first = services.order[0];
    if (!first || first === name) return;
    const grip = (e.currentTarget as HTMLButtonElement)
      .closest(".service-row")?.querySelector<HTMLElement>(".service-grip");
    moveService(name, first);
    void tick().then(() => { if (active && grip?.isConnected) grip.focus(); });
  }

  function toggleService(name: string, checked: boolean): void {
    const enabled = services.enabled.filter((s) => s !== name);
    if (checked) enabled.push(name);
    onServicesChange({ ...services, enabled });
  }

  function toggleDrawer(name: string): void {
    expandedServices[name] = !expandedServices[name];
    if (expandedServices[name] && name === "deepl" && credentials?.stored.includes("deepl-key")) {
      void loadDeeplUsage();
    }
  }

  async function loadDeeplUsage(): Promise<void> {
    try {
      const usage = await ConfigSvc.deeplUsage();
      deeplUsagePct = usage.limit > 0 ? Math.min((usage.used / usage.limit) * 100, 100) : 0;
      deeplUsageText = `本月已用 ${usage.used.toLocaleString()} / ${usage.limit.toLocaleString()} 字符`;
    } catch {
      deeplUsageText = "额度查询失败";
    }
  }

  function getDraft(serviceName: string, key: string, fallback = ""): string {
    return serviceDrafts[serviceName]?.[key] ?? fallback;
  }

  function setDraft(serviceName: string, key: string, val: string): void {
    if (!serviceDrafts[serviceName]) serviceDrafts[serviceName] = {};
    serviceDrafts[serviceName]![key] = val;
    if (testStatuses[serviceName]) testStatuses[serviceName] = { testing: false, result: "" };
  }

  function hasServiceDraft(serviceName: string): boolean {
    const drafts = serviceDrafts[serviceName];
    if (!drafts) return false;
    return Object.values(drafts).some((v) => v.trim() !== "");
  }

  function writeCredentials(serviceName: string, write: () => Promise<void>, success: string): void {
    if (credentialBusy[serviceName] || testStatuses[serviceName]?.testing) return;
    credentialBusy[serviceName] = true;
    testStatuses[serviceName] = { testing: false, result: "" };

    pendingCredentialWrite = pendingCredentialWrite.then(async () => {
      try {
        await waitForSave();
        await write();
        testStatuses[serviceName] = { testing: false, result: success };
      } catch (err) {
        testStatuses[serviceName] = { testing: false, result: `更新失败：${String(err)}`, isError: true };
      }

      try {
        await refreshCredentials();
        if (!disposed && serviceName === "deepl" && credentials?.stored.includes("deepl-key")) {
          void loadDeeplUsage();
        }
      } finally {
        credentialBusy[serviceName] = false;
      }
    });
  }

  function saveCredentials(serviceName: string): void {
    const drafts = serviceDrafts[serviceName];
    if (!drafts || !hasServiceDraft(serviceName)) return;
    writeCredentials(serviceName, async () => {
      for (const { name: key } of SERVICE_CREDENTIALS[serviceName] ?? []) {
        const value = drafts[key]?.trim();
        if (!value) continue;
        await ConfigSvc.setSecret(key, value);
        delete drafts[key];
      }
    }, "凭证已保存");
  }

  function clearSecret(serviceName: string, key: string): void {
    writeCredentials(serviceName, async () => {
      await ConfigSvc.setSecret(key, "");
      const drafts = serviceDrafts[serviceName];
      if (drafts) delete drafts[key];
    }, "凭证已清除");
  }

  async function testConnection(serviceName: string): Promise<void> {
    if (credentialBusy[serviceName] || testStatuses[serviceName]?.testing || hasServiceDraft(serviceName)) return;
    testStatuses[serviceName] = { testing: true, result: "正在测试…" };
    try {
      await waitForSave();
      const error = await ConfigSvc.testService(serviceName);
      testStatuses[serviceName] = { testing: false, result: error || "连接成功", isError: Boolean(error) };
    } catch (err) {
      testStatuses[serviceName] = { testing: false, result: `测试失败：${String(err)}`, isError: true };
    }
  }

  function saveOpenAIOption(key: keyof OpenAIOptions, value: string): void {
    onServicesChange({ ...services, openai: { ...services.openai, [key]: value.trim() } });
    delete openAIDrafts[key];
  }
</script>

<section class="settings-section" class:active id="section-services">
  <h2>翻译服务</h2>
  {#if credentials}
    <p class="service-instructions" id="service-order-instructions">
      拖动手柄，或聚焦手柄后按上下方向键，调整翻译结果的显示顺序。
    </p>
    <p class="service-settings-note">
      已启用的服务会同时翻译，凭证保存在系统密钥环。
    </p>
    <p class="service-order-status" role="status" aria-live="polite">
      {serviceOrderStatus}
    </p>

    <ol class="service-list">
      {#each services.order as name (name)}
        {@const title = SERVICE_NAMES[name] ?? name}
        {@const fields = SERVICE_CREDENTIALS[name] ?? []}
        {@const keyless = fields.length === 0}
        {@const isConfigured = credentials.configured.includes(name)}
        {@const isEnabled = services.enabled.includes(name) && (keyless || isConfigured)}
        {@const note = SERVICE_NOTES[name]}
        {@const isExpanded = Boolean(expandedServices[name])}
        {@const testStatus = testStatuses[name]}
        {@const credentialLocked = Boolean(credentialBusy[name] || testStatus?.testing)}

        {#if Object.hasOwn(SERVICE_CREDENTIALS, name)}
          <li
            class="service-row"
            class:dragging={draggedService === name}
            data-drop={dropTarget?.name === name ? (dropTarget.after ? "after" : "before") : undefined}
            ondragover={(e) => onRowDragOver(e, name)}
            ondrop={(e) => onRowDrop(e, name)}
          >
            <div class="service-card">
              <div class="field service-head">
                <div class="service-identity">
                  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
                  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                  <div
                    class="service-grip"
                    draggable="true"
                    tabindex="0"
                    role="group"
                    title="拖动排序，或按方向键移动"
                    aria-label={`排序${title}`}
                    aria-describedby="service-order-instructions"
                    aria-keyshortcuts="ArrowUp ArrowDown"
                    ondragstart={(e) => onGripDragStart(e, name)}
                    ondragend={endServiceDrag}
                    onkeydown={(e) => onGripKeydown(e, name)}
                  >
                    <svg viewBox="0 0 16 16" stroke="currentColor" stroke-width=".6" aria-hidden="true">
                      <circle cx="5.5" cy="3" r="1.2"/><circle cx="10.5" cy="3" r="1.2"/>
                      <circle cx="5.5" cy="8" r="1.2"/><circle cx="10.5" cy="8" r="1.2"/>
                      <circle cx="5.5" cy="13" r="1.2"/><circle cx="10.5" cy="13" r="1.2"/>
                    </svg>
                  </div>

                  <div class="service-name">
                    <div class="service-title-line">
                      <span class="service-title">{title}</span>
                      {#if isEnabled}
                        <span class="service-badge service-badge-enabled">已启用</span>
                      {:else if isConfigured}
                        <span class="service-badge service-badge-configured">已配置</span>
                      {:else if keyless}
                        <span class="service-badge service-badge-keyless">免配置</span>
                      {:else}
                        <span class="service-badge service-badge-unconfigured">未配置</span>
                      {/if}
                    </div>
                    <span class="sub" aria-live="polite">
                      {isEnabled ? "已启用" : keyless ? "已关闭" : isConfigured ? "已配置" : "未配置"}
                    </span>
                  </div>
                </div>

                <div class="service-actions">
                  {#if !keyless}
                    <button
                      type="button"
                      class="service-row-action"
                      aria-expanded={isExpanded}
                      aria-controls={`configure-${name}`}
                      aria-label={isExpanded ? `收起${title}配置` : `配置${title}`}
                      onclick={() => toggleDrawer(name)}
                    >
                      {isExpanded ? "收起" : "配置"}
                    </button>
                  {/if}

                  {#if isEnabled}
                    <button
                      type="button"
                      class="service-row-action service-order"
                      title="置顶"
                      aria-label={`置顶${title}`}
                      disabled={services.order[0] === name}
                      onclick={(e) => moveToTop(e, name)}
                    >
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                        <path d="M5 4h14m-7 16V8m-5 5 5-5 5 5"/>
                      </svg>
                    </button>
                  {/if}

                  <input
                    type="checkbox"
                    class="switch"
                    aria-label={`启用${title}`}
                    disabled={credentialBusy[name] || (!keyless && !isConfigured)}
                    checked={isEnabled}
                    onchange={(e) => toggleService(name, e.currentTarget.checked)}
                  />
                </div>
              </div>

              <!-- Details Drawer -->
              {#if !keyless && isExpanded}
                <div class="service-details" id={`configure-${name}`}>
                  {#if note}
                    <p class="service-note">{note}</p>
                  {/if}

                  <div class="service-link-row">
                    <button
                      type="button"
                      class="text-btn"
                      onclick={() => ConfigSvc.openKeyPage(name).catch((err) => toast(String(err)))}
                    >
                      前往控制台获取凭证 →
                    </button>
                  </div>

                  {#if name === "openai"}
                    <div class="openai-options">
                      <div class="field">
                        <label for="openai-base-url">
                          Base URL
                          <span class="sub">任何 OpenAI 兼容接口</span>
                        </label>
                        <input
                          id="openai-base-url"
                          type="text"
                          value={openAIDrafts.baseURL ?? services.openai.baseURL}
                          placeholder="https://api.openai.com/v1"
                          oninput={(e) => { openAIDrafts.baseURL = e.currentTarget.value; }}
                          onchange={(e) => saveOpenAIOption("baseURL", e.currentTarget.value)}
                        />
                      </div>

                      <div class="field">
                        <label for="openai-model">模型</label>
                        <input
                          id="openai-model"
                          type="text"
                          value={openAIDrafts.model ?? services.openai.model}
                          placeholder="gpt-4o-mini"
                          oninput={(e) => { openAIDrafts.model = e.currentTarget.value; }}
                          onchange={(e) => saveOpenAIOption("model", e.currentTarget.value)}
                        />
                      </div>
                    </div>
                  {/if}

                  <form
                    onsubmit={(e) => {
                      e.preventDefault();
                      void saveCredentials(name);
                    }}
                  >
                    {#each fields as fieldItem (fieldItem.name)}
                      {@const isStored = credentials.stored.includes(fieldItem.name)}
                      {@const draftVal = getDraft(name, fieldItem.name)}
                      <div class="field">
                        <label for={`input-${fieldItem.name}`}>
                          {fieldItem.label}
                          {#if fieldItem.name === "azure-region"}
                            <span class="sub">可选，全局资源留空</span>
                          {/if}
                        </label>
                        <div class="credential-controls">
                          <input
                            id={`input-${fieldItem.name}`}
                            type={showSecrets[`${name}-${fieldItem.name}`] ? "text" : "password"}
                            autocomplete="off"
                            placeholder={isStored ? "已保存，输入可替换" : fieldItem.placeholder}
                            value={draftVal}
                            disabled={credentialLocked}
                            oninput={(e) => setDraft(name, fieldItem.name, e.currentTarget.value)}
                          />
                          <button
                            type="button"
                            class="text-btn"
                            onclick={() => {
                              const key = `${name}-${fieldItem.name}`;
                              showSecrets[key] = !showSecrets[key];
                            }}
                          >
                            {showSecrets[`${name}-${fieldItem.name}`] ? "隐藏" : "显示"}
                          </button>
                          {#if isStored}
                            <button
                              type="button"
                              class="text-btn"
                              aria-label={`清除${fieldItem.label}`}
                              disabled={credentialLocked}
                              onclick={() => clearSecret(name, fieldItem.name)}
                            >
                              清除
                            </button>
                          {/if}
                        </div>
                      </div>
                    {/each}

                    {#if name === "deepl" && credentials.stored.includes("deepl-key")}
                      <div class="usage-wrap">
                        <div class="usage-bar">
                          <div style={`width: ${deeplUsagePct}%`}></div>
                        </div>
                        <div class="usage-text">{deeplUsageText}</div>
                      </div>
                    {/if}

                    <!-- Standardized Drawer Footer (Status on Left, Actions on Right, Save at Far Right) -->
                    <div class="service-footer">
                      <div class="service-footer-status">
                        {#if testStatus?.result}
                          <span class="usage-text" class:error-text={testStatus.isError} role="status">
                            {testStatus.result}
                          </span>
                        {:else}
                          <span class="usage-text muted">未测试</span>
                        {/if}
                      </div>

                      <div class="service-footer-actions">
                        <button
                          type="button"
                          class="service-configure"
                          disabled={!isConfigured || credentialLocked || hasServiceDraft(name)}
                          onclick={() => void testConnection(name)}
                        >
                          测试连接
                        </button>
                        <button
                          type="submit"
                          class="service-configure primary-btn"
                          disabled={!hasServiceDraft(name) || credentialLocked}
                        >
                          保存凭证
                        </button>
                      </div>
                    </div>
                  </form>
                </div>
              {/if}
            </div>
          </li>
        {/if}
      {/each}
    </ol>
  {/if}
</section>
