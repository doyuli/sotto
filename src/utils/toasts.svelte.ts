import { onDestroy } from "svelte";

export interface ToastMessage {
  id: number;
  text: string;
  fading: boolean;
}

export function createToasts() {
  const messages = $state<ToastMessage[]>([]);
  const timers = new Set<ReturnType<typeof setTimeout>>();
  let nextID = 0;
  let disposed = false;

  function schedule(callback: () => void, delay: number): void {
    const timer = setTimeout(() => {
      timers.delete(timer);
      callback();
    }, delay);
    timers.add(timer);
  }

  function show(text: string): void {
    if (disposed) return;
    const id = nextID++;
    messages.push({ id, text, fading: false });

    schedule(() => {
      const message = messages.find((item) => item.id === id);
      if (message) message.fading = true;
    }, 1200);
    schedule(() => {
      const index = messages.findIndex((item) => item.id === id);
      if (index !== -1) messages.splice(index, 1);
    }, 1600);
  }

  function clear(): void {
    for (const timer of timers) clearTimeout(timer);
    timers.clear();
    messages.length = 0;
  }

  onDestroy(() => {
    disposed = true;
    clear();
  });

  return { messages, show, clear };
}
