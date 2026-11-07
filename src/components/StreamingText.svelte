<script lang="ts">
  import { untrack } from "svelte";

  let { text, streaming }: { text: string; streaming: boolean } = $props();
  let chunks = $state<{ id: number; text: string; animate: boolean }[]>([]);
  let previous = "";
  let wasStreaming = false;
  let nextID = 0;

  $effect.pre(() => {
    const next = text;
    const active = streaming;
    untrack(() => {
      if (next !== previous) {
        // Keep earlier spans intact so new tokens never restart their animation
        // or disturb a selection in text the user is already reading.
        if (!next.startsWith(previous)) chunks = [];
        const suffix = next.startsWith(previous) ? next.slice(previous.length) : next;
        if (suffix) chunks.push({ id: nextID++, text: suffix, animate: active || wasStreaming });
        previous = next;
      }
      wasStreaming = active;
    });
  });
</script>

{#each chunks as chunk (chunk.id)}<span class:reveal={chunk.animate}>{chunk.text}</span>{/each}

<style>
  .reveal {
    animation: stream-reveal 150ms cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes stream-reveal {
    from { opacity: 0.55; }
    to { opacity: 1; }
  }

  @media (prefers-reduced-motion: reduce) {
    .reveal { animation: none; }
  }
</style>
