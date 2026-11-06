import { SnipSvc } from "../mygo";
import { el } from "../ui";

// The full-screen shot picker: a dim veil over every display, a drag
// draws the rectangle, mouseup hands it to Go (which captures, runs OCR
// and opens the bubble), Esc or a plain click cancels.

export function renderSnip(root: HTMLElement): void {
  root.replaceChildren();

  const veil = el("div", "snip-veil");
  const hint = el("div", "snip-hint", "拖拽框选要识别的文字区域，Esc 取消");
  const box = el("div", "snip-box");
  const size = el("div", "snip-size");
  box.append(size);
  root.append(veil, box, hint);

  let startX = 0;
  let startY = 0;
  let dragging = false;

  const draw = (x: number, y: number) => {
    const left = Math.min(startX, x);
    const top = Math.min(startY, y);
    const width = Math.abs(x - startX);
    const height = Math.abs(y - startY);
    box.style.left = `${left}px`;
    box.style.top = `${top}px`;
    box.style.width = `${width}px`;
    box.style.height = `${height}px`;
    size.textContent = `${width} × ${height}`;
  };

  veil.addEventListener("mousedown", (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    dragging = true;
    startX = e.clientX;
    startY = e.clientY;
    draw(startX, startY);
    box.classList.add("active");
    hint.classList.add("hidden");
  });
  veil.addEventListener("mousemove", (e) => {
    if (!dragging) return;
    draw(e.clientX, e.clientY);
  });
  veil.addEventListener("mouseup", (e) => {
    if (!dragging) return;
    dragging = false;
    const width = Math.abs(e.clientX - startX);
    const height = Math.abs(e.clientY - startY);
    if (width < 8 || height < 8) {
      void SnipSvc.done(null); // a click, not a drag
      return;
    }
    void SnipSvc.done({
      x: Math.min(startX, e.clientX),
      y: Math.min(startY, e.clientY),
      w: width,
      h: height,
    });
  });
  document.addEventListener(
    "keydown",
    (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        void SnipSvc.done(null);
      }
    },
    { once: true },
  );
}
