// @vitest-environment jsdom
import { afterEach, expect, test, vi } from "vitest";
import { scrollActivity } from "../src/utils/scroll-activity";

afterEach(() => vi.useRealTimers());

test("scrollbars appear only while scrolling and wait for the last scroll to hide", () => {
  vi.useFakeTimers();
  const node = document.createElement("div");
  const action = scrollActivity(node);
  node.dispatchEvent(new MouseEvent("mouseenter"));
  expect(node.classList.contains("scrolling")).toBe(false);
  node.dispatchEvent(new Event("scroll"));
  expect(node.classList.contains("scrolling")).toBe(true);
  vi.advanceTimersByTime(800);
  node.dispatchEvent(new Event("scroll"));
  vi.advanceTimersByTime(800);
  expect(node.classList.contains("scrolling")).toBe(true);
  vi.advanceTimersByTime(100);
  expect(node.classList.contains("scrolling")).toBe(false);
  node.dispatchEvent(new Event("scroll"));
  action.destroy();
  expect(vi.getTimerCount()).toBe(0);
  node.dispatchEvent(new Event("scroll"));
  expect(node.classList.contains("scrolling")).toBe(false);
});
