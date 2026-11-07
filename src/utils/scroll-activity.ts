// Keep scrollbar visibility independent of layout and input method. Keyboard,
// wheel, touchpad, and scrollbar dragging all produce the same scroll event.
export function scrollActivity(node: HTMLElement): { destroy: () => void } {
  let timer: ReturnType<typeof setTimeout> | undefined;
  function onScroll(): void {
    node.classList.add("scrolling");
    clearTimeout(timer);
    timer = setTimeout(() => node.classList.remove("scrolling"), 900);
  }
  node.addEventListener("scroll", onScroll, { passive: true });
  return {
    destroy() {
      node.removeEventListener("scroll", onScroll);
      clearTimeout(timer);
      node.classList.remove("scrolling");
    },
  };
}
