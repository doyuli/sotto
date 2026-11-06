<script lang="ts">
  import { onMount } from "svelte";
  import { SnipSvc } from "../mygo";

  let sessionID = $state<number | null>(null);
  let dragging = $state(false);
  let submitted = false;

  let startX = $state(0);
  let startY = $state(0);
  let currentX = $state(0);
  let currentY = $state(0);

  let veilElem: HTMLDivElement | undefined = $state();

  let rect = $derived.by(() => {
    const x = Math.min(startX, currentX);
    const y = Math.min(startY, currentY);
    const w = Math.abs(currentX - startX);
    const h = Math.abs(currentY - startY);
    return { x, y, w, h };
  });

  function parseSession(): number | null {
    const raw = new URLSearchParams(location.hash.slice(1)).get("t");
    const id = Number(raw);
    return Number.isSafeInteger(id) && id > 0 ? id : null;
  }

  function finish(result: { x: number; y: number; w: number; h: number } | null): void {
    if (submitted || sessionID === null) return;
    submitted = true;
    dragging = false;
    void SnipSvc.done(sessionID, result);
  }

  function clampPoint(e: PointerEvent): { x: number; y: number } {
    return {
      x: Math.max(0, Math.min(window.innerWidth, e.clientX)),
      y: Math.max(0, Math.min(window.innerHeight, e.clientY)),
    };
  }

  function handlePointerDown(e: PointerEvent): void {
    if (e.button !== 0 || submitted || sessionID === null) return;
    e.preventDefault();
    const p = clampPoint(e);
    startX = p.x;
    startY = p.y;
    currentX = p.x;
    currentY = p.y;
    dragging = true;
    veilElem?.setPointerCapture(e.pointerId);
  }

  function handlePointerMove(e: PointerEvent): void {
    if (!dragging) return;
    const p = clampPoint(e);
    currentX = p.x;
    currentY = p.y;
  }

  function handlePointerUp(e: PointerEvent): void {
    if (!dragging) return;
    dragging = false;
    const p = clampPoint(e);
    const w = Math.abs(p.x - startX);
    const h = Math.abs(p.y - startY);
    if (w < 8 || h < 8) {
      finish(null);
    } else {
      finish({
        x: Math.round(Math.min(startX, p.x)),
        y: Math.round(Math.min(startY, p.y)),
        w: Math.round(w),
        h: Math.round(h),
      });
    }
  }

  function handlePointerCancel(): void {
    if (dragging) finish(null);
  }

  function syncHash(): void {
    submitted = false;
    dragging = false;
    sessionID = parseSession();
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (sessionID !== null && e.key === "Escape") {
      e.preventDefault();
      finish(null);
    }
  }

  function handleContextMenu(e: MouseEvent): void {
    if (sessionID === null) return;
    e.preventDefault();
    finish(null);
  }

  onMount(syncHash);
</script>

<svelte:window onhashchange={syncHash} />
<svelte:document onkeydown={handleKeydown} oncontextmenu={handleContextMenu} />

{#if sessionID !== null}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    bind:this={veilElem}
    class="snip-veil"
    onpointerdown={handlePointerDown}
    onpointermove={handlePointerMove}
    onpointerup={handlePointerUp}
    onpointercancel={handlePointerCancel}
  ></div>

  <div
    class="snip-box"
    class:active={dragging}
    style:left={`${rect.x}px`}
    style:top={`${rect.y}px`}
    style:width={`${rect.w}px`}
    style:height={`${rect.h}px`}
  >
    {#if dragging}
      <div class="snip-size">
        {Math.round(rect.w)} × {Math.round(rect.h)}
      </div>
    {/if}
  </div>

  <div class="snip-hint" class:hidden={dragging}>
    拖拽框选文字，Esc 或右键取消；跨屏文字请分次框选
  </div>
{/if}
