// Small DOM helpers shared by the pages.

/** el makes an element, with a class and text when given. */
export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

/** debounce returns a function that runs fn after quiet milliseconds. */
export function debounce<A extends unknown[]>(fn: (...args: A) => void, quiet: number): (...args: A) => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return (...args: A) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), quiet);
  };
}

/** toast shows a message at the bottom of the page, briefly. */
export function toast(text: string): void {
  let host = document.querySelector<HTMLElement>(".toast-host");
  if (!host) {
    host = el("div", "toast-host");
    document.body.append(host);
  }
  const item = el("div", "toast", text);
  host.append(item);
  setTimeout(() => item.classList.add("gone"), 1200);
  setTimeout(() => item.remove(), 1600);
}

/** requestID identifies one translation run; getRandomValues works in
 * the packaged app, unlike crypto.randomUUID's secure context. */
export function requestID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(8));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
