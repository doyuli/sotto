<script module lang="ts">
  import type { FormatOption } from "../mygo";

  export interface CardState {
    status: "loading" | "streaming" | "ok" | "error";
    text?: string;
    err?: string;
    formats?: FormatOption[];
  }
</script>

<script lang="ts">
  import BubbleIcon from "./BubbleIcon.svelte";

  let { name, title, card, isCollapsed, onToggle, onRetry, onOpenSettings, onSpeak, onCopy }: {
    name: string;
    title: string;
    card: CardState;
    isCollapsed: boolean;
    onToggle: () => void;
    onRetry: () => void;
    onOpenSettings: () => void;
    onSpeak: (text: string) => void;
    onCopy: (text: string, successMessage?: string) => void;
  } = $props();

  function formatIcon(style: string): string {
    switch (style) {
      case "lowerCamel": return "aB";
      case "upperCamel": return "AB";
      case "snake": return "a_b";
      case "constant": return "A_B";
      case "kebab": return "a-b";
      default: return style.slice(0, 2);
    }
  }
</script>

<div class="card card-{card.status}" class:collapsed={isCollapsed}>
  <div class="card-head">
    <span class="card-name">{title}</span>
    <span class="card-status" role="status">{card.status === "streaming" ? "生成中" : ""}</span>
    <button
      type="button"
      class="icon-btn card-toggle"
      title="折叠"
      aria-expanded={!isCollapsed}
      aria-label={`${isCollapsed ? "展开" : "折叠"}${title}`}
      onclick={onToggle}
    >
      <BubbleIcon name="chevron" />
    </button>
  </div>

  <div class="card-body" aria-busy={card.status === "loading" || card.status === "streaming"}>
    {#if card.status === "loading"}
      <div class="skeleton"></div>
      <div class="skeleton short"></div>
    {:else}
      {#if card.text}
        <div class="card-text">{card.text}</div>
      {/if}
      {#if card.status === "error"}
        <div class="card-error" role="status">{card.text ? "译文未完成。" : ""}{card.err ?? "出错了"}</div>
        <div class="card-error-actions">
          <button type="button" class="text-btn" onclick={onRetry}>重试</button>
          {#if name === "google" || name === "index-translate"}
            <button type="button" class="text-btn" onclick={onOpenSettings}>
              配置翻译服务
            </button>
          {/if}
        </div>
      {/if}
      {#if card.text}
        <div class="card-tools">
          {#if card.formats?.length}
            {#each card.formats as f (f.style)}
              <button
                type="button"
                class="format-btn"
                title={`${f.label}：${f.value}`}
                onclick={() => onCopy(f.value, `已复制 ${f.label}`)}
              >
                <span class="format-icon">{formatIcon(f.style)}</span>
              </button>
            {/each}
          {/if}
          <button
            type="button"
            class="icon-btn"
            title={card.status === "ok" ? "朗读" : "译文完成后可朗读"}
            aria-label="朗读"
            disabled={card.status !== "ok"}
            onclick={() => onSpeak(card.text ?? "")}
          >
            <BubbleIcon name="speak" />
          </button>
          <button
            type="button"
            class="icon-btn"
            title={card.status === "ok" ? "复制" : "译文完成后可复制"}
            aria-label="复制"
            disabled={card.status !== "ok"}
            onclick={() => onCopy(card.text ?? "")}
          >
            <BubbleIcon name="copy" />
          </button>
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .card {
    flex-shrink: 0;
    border-radius: 12px;
    background: var(--card-body);
    overflow: hidden;
  }

  /* The title strip is deeper than the body in both themes. */
  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 14px;
    background: var(--card-head);
    font-size: 13px;
    color: var(--muted);
  }

  .card-status {
    margin-left: auto;
    padding-right: 8px;
    font-size: 12px;
    color: var(--muted);
  }

  .card-name {
    color: var(--fg);
    font-weight: 500;
    opacity: 0.9;
  }

  .card-toggle {
    color: var(--muted);
    opacity: 1;
    transition: transform 0.15s;
  }

  .card-toggle:hover {
    background: var(--hover);
    color: var(--fg);
  }

  .card-toggle :global(svg) {
    width: 14px;
    height: 14px;
  }

  .card.collapsed .card-toggle {
    transform: rotate(90deg);
  }

  .card.collapsed .card-body {
    display: none;
  }

  .card-body {
    background: var(--card-body);
    padding: 12px 14px 10px;
  }

  .card-text {
    font-size: 15px;
    line-height: 1.65;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .card-body > .card-error {
    font-size: 12px;
    color: var(--error);
    padding: 2px 0;
  }

  .skeleton {
    height: 13px;
    border-radius: 6px;
    background: linear-gradient(90deg, var(--card-head) 25%, var(--card-body) 50%, var(--card-head) 75%);
    background-size: 200% 100%;
    animation: shimmer 1.2s infinite;
    margin: 6px 0;
  }

  .skeleton.short {
    width: 55%;
  }

  @keyframes shimmer {
    from {
      background-position: 200% 0;
    }
    to {
      background-position: -200% 0;
    }
  }

  .card-tools {
    display: flex;
    align-items: center;
    gap: 2px;
    padding-top: 4px;
  }

  .format-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 24px;
    height: 24px;
    padding: 0 4px;
    border: none;
    background: transparent;
    color: var(--icon);
    border-radius: 6px;
    cursor: pointer;
  }

  .format-btn:hover {
    background: var(--hover);
    color: var(--icon-strong);
  }

  .format-icon {
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    font-size: 10px;
    line-height: 1;
    letter-spacing: -0.02em;
  }

  .card .icon-btn {
    color: var(--icon);
    opacity: 1;
  }

  .card .icon-btn:hover {
    background: var(--hover);
    color: var(--icon-strong);
  }

  .card .icon-btn:disabled {
    opacity: 0.45;
    cursor: default;
    background: transparent;
    color: var(--icon);
  }

  .card-error-actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
  }
</style>
