<script lang="ts">
  import type { ToastMessage } from "../utils/toasts.svelte";

  let { messages }: { messages: ToastMessage[] } = $props();
</script>

<div class="toast-host" role="status" aria-atomic="false">
  {#each messages as message (message.id)}
    <div class="toast" class:gone={message.fading}>{message.text}</div>
  {/each}
</div>

<style>
  .toast-host {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 10px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    pointer-events: none;
  }

  .toast {
    background: var(--fg);
    color: var(--bg);
    font-size: 12px;
    border-radius: 999px;
    padding: 5px 14px;
    opacity: 1;
    transition: opacity 0.4s;
  }

  .toast.gone {
    opacity: 0;
  }
</style>
