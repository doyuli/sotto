import { SnipSvc } from "../mygo";
import { el } from "../ui";

// Every mount owns its listeners; changing the screenshot session aborts them.
let listeners: AbortController | undefined;
export function renderSnip(root: HTMLElement): void {
  listeners?.abort();
  listeners = new AbortController();
  const { signal } = listeners;
  const session = Number(new URLSearchParams(location.hash.split("?")[1] ?? "").get("t"));
  root.replaceChildren();
  const veil = el("div", "snip-veil");
  const hint = el("div", "snip-hint", "拖拽框选文字，Esc 或右键取消；跨屏文字请分次框选");
  const box = el("div", "snip-box");
  const size = el("div", "snip-size");
  box.append(size);
  root.append(veil, box, hint);
  let startX = 0, startY = 0, dragging = false, submitted = false;
  const point = (e: PointerEvent) => ({ x: Math.max(0, Math.min(innerWidth, e.clientX)), y: Math.max(0, Math.min(innerHeight, e.clientY)) });
  const finish = (rect: { x: number; y: number; w: number; h: number } | null) => {
    if (submitted) return;
    submitted = true;
    listeners?.abort();
    void SnipSvc.done(session, rect);
  };
  const draw = (x: number, y: number) => {
    box.style.left = `${Math.min(startX, x)}px`;
    box.style.top = `${Math.min(startY, y)}px`;
    box.style.width = `${Math.abs(x-startX)}px`;
    box.style.height = `${Math.abs(y-startY)}px`;
    size.textContent = `${Math.round(Math.abs(x-startX))} × ${Math.round(Math.abs(y-startY))}`;
  };
  veil.addEventListener("pointerdown", (e) => {
    if (e.button !== 0 || submitted) return;
    e.preventDefault();
    const p = point(e); startX = p.x; startY = p.y; dragging = true;
    veil.setPointerCapture(e.pointerId);
    draw(startX, startY); box.classList.add("active"); hint.classList.add("hidden");
  }, { signal });
  veil.addEventListener("pointermove", (e) => { if (dragging) { const p = point(e); draw(p.x,p.y); } }, { signal });
  veil.addEventListener("pointerup", (e) => {
    if (!dragging) return;
    dragging = false;
    const p = point(e), w = Math.abs(p.x-startX), h = Math.abs(p.y-startY);
    finish(w < 8 || h < 8 ? null : {x:Math.round(Math.min(startX,p.x)), y:Math.round(Math.min(startY,p.y)), w:Math.round(w), h:Math.round(h)});
  }, { signal });
  veil.addEventListener("pointercancel", () => finish(null), { signal });
  document.addEventListener("contextmenu", (e) => { e.preventDefault(); finish(null); }, { signal });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") { e.preventDefault(); finish(null); }
  }, { signal });
}
