// @vitest-environment jsdom
import { expect, test } from "vitest";
import { mount, tick } from "svelte";
import SnipPage from "../src/components/SnipPage.svelte";
import { setupPageTest } from "./support/page";

const page = setupPageTest();

function keydown(key: string): void {
  document.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
}

test("Escape cancels the current Go session once and a hash change starts the next one", async () => {
  history.replaceState(null, "", "/snip.html#t=42");
  page.track(mount(SnipPage, { target: page.target }));
  await tick();
  expect(page.target.querySelector(".snip-veil")).not.toBeNull();

  keydown("Shift");
  keydown("Escape");
  keydown("Escape");
  expect(page.backend.calls("SnipSvc.Done")).toEqual([[42, null]]);

  history.replaceState(null, "", "/snip.html#t=43");
  window.dispatchEvent(new Event("hashchange"));
  await tick();
  document.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true }));
  expect(page.backend.calls("SnipSvc.Done")).toEqual([[42, null], [43, null]]);

  await page.destroy();
  history.replaceState(null, "", "/snip.html#t=44");
  window.dispatchEvent(new Event("hashchange"));
  keydown("Escape");
  document.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true }));
  expect(page.backend.calls("SnipSvc.Done")).toHaveLength(2);
});
