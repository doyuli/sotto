<script lang="ts">
  import { onMount, tick } from "svelte";
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";
  import { MediaQuery } from "svelte/reactivity";
  import { dragHandle, dragHandleZone, SHADOW_ITEM_MARKER_PROPERTY_NAME, TRIGGERS, type DndEvent } from "svelte-dnd-action";
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
  let credentialsError = $state<string>("");
  let openAIDrafts = $state<Partial<OpenAIOptions>>({});

  let expandedServices = $state<Record<string, boolean>>({});
  let serviceDrafts = $state<Record<string, Record<string, string>>>({});
  let showSecrets = $state<Record<string, boolean>>({});
  let credentialBusy = $state<Record<string, boolean>>({});
  let testStatuses = $state<Record<string, { testing: boolean; result: string; isError?: boolean }>>({});
  let deeplUsageText = $state<string>("额度查询中…");
  let deeplUsagePct = $state<number>(0);

  const credentialNames = $derived(services.order.filter((s) => Object.hasOwn(SERVICE_CREDENTIALS, s)));

  // A service is on when it is enabled and has what it needs to run. Those
  // stay first in the list; the rest form a separate, unsortable group.
  function serviceOn(name: string): boolean {
    const keyless = (SERVICE_CREDENTIALS[name] ?? []).length === 0;
    return services.enabled.includes(name) && (keyless || Boolean(credentials?.configured.includes(name)));
  }

  const enabledNames = $derived(credentialNames.filter((name) => serviceOn(name)));
  const disabledNames = $derived(credentialNames.filter((name) => !serviceOn(name)));

  type ServiceItem = { id: string; name: string; [SHADOW_ITEM_MARKER_PROPERTY_NAME]?: boolean };

  const reducedMotion = new MediaQuery("prefers-reduced-motion: reduce");
  const flipDurationMs = $derived(reducedMotion.current ? 0 : 150);
  let dragPreview = $state.raw<{ order: string[]; items: ServiceItem[] } | null>(null);
  const currentPreview = $derived.by(() => {
    if (!active || !dragPreview || enabledNames.length !== dragPreview.order.length) return null;
    return dragPreview.order.every((name, index) => name === enabledNames[index]) ? dragPreview : null;
  });
  const sortableItems = $derived(currentPreview?.items ?? enabledNames.map((name): ServiceItem => ({ id: name, name })));
  let serviceOrderStatus = $state<string>("");
  let serviceSection: HTMLElement;

  let pendingCredentialWrite = Promise.resolve();
  let disposed = false;

  async function refreshCredentials(): Promise<void> {
    if (disposed) return;
    try {
      const status = await ConfigSvc.credentials();
      if (!disposed) {
        credentials = status;
        credentialsError = "";
      }
    } catch (err) {
      if (!disposed) {
        const message = err instanceof Error ? err.message : String(err);
        credentialsError = `读取服务状态失败：${message}`;
        toast(credentialsError);
      }
    }
  }

  onMount(() => {
    void refreshCredentials();
    return () => { disposed = true; };
  });

  $effect(() => {
    // Late events from a cancelled drag must not revive its preview.
    if (!currentPreview) dragPreview = null;
  });

  $effect.pre(() => {
    if (!active || enabledNames.length + disabledNames.length === 0) return;
    const focused = document.activeElement;
    if (!(focused instanceof HTMLInputElement) || !serviceSection?.contains(focused)) return;
    const name = focused.dataset.serviceToggle;
    if (!name) return;

    // Both an optimistic toggle and its rollback can move the switch to another list.
    void tick().then(() => {
      if (active && !focused.isConnected && document.activeElement === document.body) {
        serviceSection.querySelector<HTMLInputElement>(`[data-service-toggle="${name}"]`)?.focus();
      }
    });
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
    const position = order.slice(0, to + 1).filter((s) => serviceOn(s)).length;
    serviceOrderStatus = `${SERVICE_NAMES[name] ?? name} 已移至第 ${position} 位`;
    onServicesChange({ ...services, order });
  }

  function previewServiceOrder({ detail: { items, info } }: CustomEvent<DndEvent<ServiceItem>>): void {
    if (!active) return;
    if (info.trigger === TRIGGERS.DRAG_STARTED) {
      dragPreview = { order: enabledNames, items };
    } else if (currentPreview) {
      dragPreview = { ...currentPreview, items };
    }
  }

  function finalizeServiceOrder({ detail: { items, info } }: CustomEvent<DndEvent<ServiceItem>>): void {
    const preview = currentPreview;
    dragPreview = null;
    if (!preview || info.trigger !== TRIGGERS.DROPPED_INTO_ZONE) return;
    const from = enabledNames.indexOf(info.id);
    const to = items.findIndex((item) => item.id === info.id);
    if (from < 0 || to < 0 || from === to || !enabledNames[to]) return;
    moveService(info.id, enabledNames[to], to > from);
  }

  function suppressKeyboardDrag(e: KeyboardEvent): void {
    // The library starts a keyboard drag on Space or Enter.
    if (e.key === " " || e.key === "Enter") {
      e.stopImmediatePropagation();
    }
  }

  function toggleService(name: string, checked: boolean): void {
    const enabled = services.enabled.filter((s) => s !== name);
    if (checked) enabled.push(name);
    else expandedServices[name] = false; // the row moves to the off group
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
      const success = serviceName === "openai" ? "连接成功（未验证流式输出）" : "连接成功";
      testStatuses[serviceName] = { testing: false, result: error || success, isError: Boolean(error) };
    } catch (err) {
      testStatuses[serviceName] = { testing: false, result: `测试失败：${String(err)}`, isError: true };
    }
  }

  function saveOpenAIOption(key: keyof OpenAIOptions, value: string): void {
    onServicesChange({ ...services, openai: { ...services.openai, [key]: value.trim() } });
    delete openAIDrafts[key];
  }
</script>

<section
  class="settings-section"
  class:active
  class:service-drag-preview={currentPreview !== null}
  id="section-services"
  bind:this={serviceSection}
>
  <h2>翻译服务</h2>
  {#if credentials}
    <div class="service-instructions" id="service-order-instructions">
      <p>已启用的服务会同时翻译，序号即结果顺序。</p>
      <p class="sub">拖动序号调整顺序，凭证保存在系统密钥环。</p>
    </div>
    <p class="service-order-status" role="status" aria-live="polite">
      {serviceOrderStatus}
    </p>

    {#snippet serviceRow(name: string, enabled: boolean, position: number)}
        {@const title = SERVICE_NAMES[name] ?? name}
        {@const fields = SERVICE_CREDENTIALS[name] ?? []}
        {@const keyless = fields.length === 0}
        {@const isConfigured = credentials?.configured.includes(name) ?? false}
        {@const note = SERVICE_NOTES[name]}
        {@const isExpanded = Boolean(expandedServices[name])}
        {@const testStatus = testStatuses[name]}
        {@const credentialLocked = Boolean(credentialBusy[name] || testStatus?.testing)}
        {@const stateHint = enabled
          ? (keyless ? "无需凭据" : "凭证已保存")
          : (keyless || isConfigured ? "打开开关后参与翻译" : "填写凭证后可启用")}

            <div class="service-card">
              <div class="field service-head">
                <div class="service-identity">
                  {#if enabled}
                    <!-- The drag zone ignores events that start on form controls. -->
                    <span
                      class="service-grip"
                      use:dragHandle
                      role="button"
                      tabindex="0"
                      title="拖动排序"
                      aria-label={`排序${title}，当前第 ${position} 位`}
                      aria-describedby="service-order-instructions"
                      onkeydowncapture={suppressKeyboardDrag}
                    >
                      <span aria-hidden="true">{position}</span>
                    </span>
                  {/if}

                  <div class="service-name">
                    <div class="service-title-line">
                      <span class="service-title">{title}</span>
                      {#if !enabled}
                        {#if keyless}
                          <span class="service-badge service-badge-keyless">免配置</span>
                        {:else if isConfigured}
                          <span class="service-badge service-badge-configured">已配置</span>
                        {:else}
                          <span class="service-badge service-badge-unconfigured">未配置</span>
                        {/if}
                      {/if}
                    </div>
                    <span class="sub" aria-live="polite">
                      {stateHint}
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

                  <input
                    type="checkbox"
                    class="switch"
                    data-service-toggle={name}
                    aria-label={`启用${title}`}
                    disabled={credentialBusy[name] || (!keyless && !isConfigured)}
                    checked={enabled}
                    onchange={(e) => toggleService(name, e.currentTarget.checked)}
                  />
                </div>
              </div>

              <!-- Details Drawer -->
              {#if !keyless && isExpanded}
                <div class="service-details" id={`configure-${name}`}>
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
                      {@const isStored = credentials?.stored.includes(fieldItem.name) ?? false}
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
                            placeholder={isStored ? "已保存，留空不变" : fieldItem.placeholder}
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

                    {#if note}
                      <p class="service-note">{note}</p>
                    {/if}

                    {#if name === "deepl" && (credentials?.stored.includes("deepl-key") ?? false)}
                      <div class="usage-wrap">
                        <div class="usage-text">{deeplUsageText}</div>
                        <div class="usage-bar">
                          <div style={`width: ${deeplUsagePct}%`}></div>
                        </div>
                      </div>
                    {/if}

                    <!-- Standardized Drawer Footer (Status on Left, Actions on Right, Save at Far Right) -->
                    <div class="service-footer">
                      <div class="service-footer-status">
                        {#if testStatus?.result}
                          <span
                            class="usage-text"
                            class:error-text={testStatus.isError}
                            class:success-text={!testStatus.isError && !testStatus.testing}
                            role="status"
                          >
                            {testStatus.result}
                          </span>
                        {:else}
                          <span class="usage-text">未测试</span>
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
    {/snippet}

    <h3 class="service-group-title">
      已启用 <span class="service-group-count">{enabledNames.length}</span>
    </h3>
    {#if sortableItems.length}
      <ol
        class="service-list"
        aria-label="已启用的翻译服务"
        use:dragHandleZone={{
          items: sortableItems,
          flipDurationMs,
          dragDisabled: !active,
          dropAnimationDisabled: reducedMotion.current || !currentPreview,
          dropTargetStyle: {},
          autoAriaDisabled: true,
          useCursorForDetection: true,
          morphDisabled: true,
          transformDraggedElement: (element, _item, index) => {
            const number = element?.querySelector(".service-grip span");
            if (number && index !== undefined) number.textContent = String(index + 1);
          },
        }}
        onconsider={previewServiceOrder}
        onfinalize={finalizeServiceOrder}
      >
        {#each sortableItems as item, index (item.id)}
          <li
            class="service-row"
            data-is-dnd-shadow-item-hint={item[SHADOW_ITEM_MARKER_PROPERTY_NAME]}
            inert={item[SHADOW_ITEM_MARKER_PROPERTY_NAME]}
            animate:flip={{ duration: flipDurationMs, easing: cubicOut }}
          >
            {@render serviceRow(item.name, true, index + 1)}
          </li>
        {/each}
      </ol>
    {:else}
      <p class="service-empty">还没有启用的服务。打开下方开关即可加入翻译。</p>
    {/if}

    {#if disabledNames.length}
      <h3 class="service-group-title">
        未启用 <span class="service-group-count">{disabledNames.length}</span>
      </h3>
      <ol class="service-list">
        {#each disabledNames as name (name)}
          <li class="service-row">
            {@render serviceRow(name, false, 0)}
          </li>
        {/each}
      </ol>
    {/if}
  {:else}
    <div class="service-loading" role="status">
      <span>{credentialsError || "正在读取服务状态…"}</span>
      {#if credentialsError}
        <button type="button" class="service-configure" onclick={() => void refreshCredentials()}>
          重试
        </button>
      {/if}
    </div>
  {/if}
</section>
