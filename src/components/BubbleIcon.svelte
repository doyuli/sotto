<script lang="ts">
  let { name, filled = false }: {
    name: "pin" | "close" | "swap" | "speak" | "copy" | "chevron";
    filled?: boolean;
  } = $props();
</script>

<svg viewBox="0 0 24 24" fill={filled ? "currentColor" : "none"} stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  {#if name === "pin"}
    <path d="M12 17v5"/>
    <path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z"/>
  {:else if name === "close"}
    <path d="M18 6 6 18"/>
    <path d="m6 6 12 12"/>
  {:else if name === "swap"}
    <path d="m16 3 4 4-4 4"/>
    <path d="M20 7H4"/>
    <path d="m8 21-4-4 4-4"/>
    <path d="M4 17h16"/>
  {:else if name === "speak"}
    <path d="M11 5 6 9H2v6h4l5 4V5z"/>
    <path d="M15.54 8.46a5 5 0 0 1 0 7.07"/>
  {:else if name === "copy"}
    <rect width="14" height="14" x="8" y="8" rx="2" ry="2"/>
    <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>
  {:else if name === "chevron"}
    <path d="m6 9 6 6 6-6"/>
  {/if}
</svg>
