// @vitest-environment jsdom
import { expect, test, vi } from "vitest";
import { mount, tick } from "svelte";
import BubblePage from "../src/components/BubblePage.svelte";
import type { BubbleState, FormatOption, RunReq } from "../src/mygo";
import { deferred, settle, setupPageTest } from "./support/page";

const page = setupPageTest();

function input(): HTMLTextAreaElement {
  return page.target.querySelector("textarea")!;
}

function button(name: string, root: ParentNode = page.target): HTMLButtonElement {
  const found = [...root.querySelectorAll("button")].find((node) =>
    node.textContent?.trim() === name || node.title === name || node.getAttribute("aria-label") === name
  );
  if (!found) throw new Error(`Missing button: ${name}`);
  return found;
}

async function openBubble(): Promise<void> {
  page.track(mount(BubblePage, { target: page.target, props: { id: 1 } }));
  await vi.waitFor(() => expect(input().value).toBe(page.backend.bubble.text));
  await settle();
}

function edit(text: string): void {
  input().value = text;
  input().dispatchEvent(new Event("input", { bubbles: true }));
}

function keydown(key: string, target: EventTarget = document): void {
  target.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
}

function run(text: string): RunReq {
  edit(text);
  keydown("Enter", input());
  return lastRun();
}

function lastRun(): RunReq {
  return page.backend.calls("TranslateSvc.Run").at(-1)![0] as RunReq;
}

function update(service: string, text: string, reqID = "initial", err = ""): void {
  page.backend.emit("translation-update", { reqID, service, text, err, detectedLang: "en" });
}

function cardTexts(): (string | null)[] {
  return [...page.target.querySelectorAll(".card-text")].map((node) => node.textContent);
}

function present(showID: number, revision = 1, clipboardRecoveryID = ""): void {
  page.backend.emit("bubble-presented", {
    sessionID: page.backend.bubble.sessionID, showID, revision, clipboardRecoveryID, focus: true,
  });
}

test("another result preserves card identity, scroll and focus", async () => {
  await openBubble();
  update("google", "你好");
  await tick();
  const card = page.target.querySelector(".card-ok")!;
  const scroll = page.target.querySelector(".bubble-scroll")!;
  scroll.scrollTop = 80;
  const copy = button("复制", card);
  copy.focus();

  update("baidu", "您好");
  await tick();
  expect(page.target.querySelector(".card-ok")).toBe(card);
  expect(scroll.scrollTop).toBe(80);
  expect(document.activeElement).toBe(copy);
});

test("retry requests only the failed service and retains the successful card", async () => {
  await openBubble();
  update("google", "你好");
  update("baidu", "", "initial", "网络错误");
  await tick();
  const card = page.target.querySelector(".card-ok");
  button("重试").click();
  await tick();
  expect(lastRun().service).toBe("baidu");
  expect(page.target.querySelector(".card-ok")).toBe(card);
});

test("language changes use the new selection and deleting the last character cancels", async () => {
  await openBubble();
  for (const [selector, property, language, label] of [
    [".source-select", "sourceLang", "fr", "法语"],
    [".lang-select", "targetLang", "en", "英语"],
  ] as const) {
    const select = page.target.querySelector<HTMLSelectElement>(selector)!;
    select.value = language;
    select.dispatchEvent(new Event("change", { bubbles: true }));
    expect(lastRun()[property]).toBe(language);
    await tick();
    expect(select.closest("label")?.querySelector(".lang-value")?.textContent).toBe(label);
  }
  edit("");
  await tick();
  expect(page.backend.calls("TranslateSvc.Cancel")).toEqual([["test-session", 4]]);
  expect(page.target.querySelector(".card")).toBeNull();
});

test("clear button cancels and leaves an editable composer", async () => {
  await openBubble();
  button("清空输入").click();
  await tick();
  expect(input().value).toBe("");
  expect(input().readOnly).toBe(false);
  expect(page.backend.calls("TranslateSvc.Cancel")).toEqual([["test-session", 2]]);
});

test("late results and capture resets cannot replace a manual translation", async () => {
  await openBubble();
  const request = run("new text");
  expect(request.revision).toBe(2);
  page.backend.emit("bubble-reset", { ...page.backend.bubble, revision: 1, text: "late OCR" });
  update("google", "old result");
  await tick();
  expect(input().value).toBe("new text");
  expect(cardTexts()).toEqual([]);
  update("google", "new result", request.reqID);
  await tick();
  expect(cardTexts()).toEqual(["new result"]);
});

test("cancelled OCR stays editable when a late recognition snapshot arrives", async () => {
  await openBubble();
  const recognizing = {
    ...page.backend.bubble, sessionID: "ocr", generation: 2, reqID: "ocr",
    revision: -1, trigger: "ocr", phase: "recognizing", text: "",
  };
  page.backend.emit("bubble-reset", recognizing);
  await tick();
  expect(input().readOnly).toBe(true);
  expect(page.target.querySelector(".ocr-loading")?.getAttribute("aria-hidden")).toBe("true");
  expect(page.target.querySelector(".composer")?.getAttribute("aria-busy")).toBe("true");
  button("取消识别").click();
  await tick();
  expect(input().readOnly).toBe(false);
  expect(page.target.querySelector(".ocr-loading")).toBeNull();
  expect(page.target.querySelector(".composer")?.getAttribute("aria-busy")).toBe("false");
  expect(button("重新框选")).toBeDefined();
  page.backend.emit("bubble-reset", { ...recognizing, text: "late capture" });
  await tick();
  expect(input().value).toBe("");
});

test("OCR loading state ends when recognition completes or fails", async () => {
  page.backend.bubble = { ...page.backend.bubble, trigger: "ocr", phase: "recognizing", text: "" };
  await openBubble();
  expect(page.target.querySelector(".capture-status")?.textContent).toContain("正在识别文字");
  expect(page.target.querySelector(".ocr-loading")).not.toBeNull();
  page.backend.emit("bubble-reset", { ...page.backend.bubble, revision: 2, phase: "", text: "recognized" });
  await tick();
  expect(page.target.querySelector(".ocr-loading")).toBeNull();
  page.backend.emit("bubble-reset", { ...page.backend.bubble, generation: 2, sessionID: "next-ocr", phase: "recognizing" });
  await tick();
  expect(page.target.querySelector(".ocr-loading")).not.toBeNull();
  page.backend.emit("bubble-reset", { ...page.backend.bubble, generation: 2, sessionID: "next-ocr", revision: 3, phase: "error", message: "文字识别失败，请重新框选。" });
  await tick();
  expect(page.target.querySelector(".ocr-loading")).toBeNull();
  expect(page.target.querySelector(".capture-status")?.textContent).toContain("文字识别失败");
});

test("Ready results survive an older reset queued before the snapshot returns", async () => {
  const ready = deferred<BubbleState>();
  page.backend.override("BubbleSvc.Ready", () => ready.promise);
  page.track(mount(BubblePage, { target: page.target, props: { id: 1 } }));
  await tick();
  page.backend.emit("bubble-reset", { ...page.backend.bubble, generation: 0, text: "old session" });
  ready.resolve({
    ...page.backend.bubble,
    results: [{ reqID: "initial", service: "google", text: "你好", detectedLang: "en", err: "", streaming: false }],
  });
  await settle();
  expect(input().value).toBe(page.backend.bubble.text);
  expect(cardTexts()).toEqual(["你好"]);
});

test("native stop invalidates pending results and the next run advances the revision", async () => {
  await openBubble();
  page.backend.emit("run-stopped", { sessionID: "test-session", showID: 1, revision: 3 });
  await tick();
  expect(page.target.querySelector(".card-loading")).toBeNull();
  expect(run("resume").revision).toBe(4);
  update("google", "late");
  await tick();
  expect(cardTexts()).toEqual([]);
});

test("a retry can receive both the peer's original result and its own result", async () => {
  await openBubble();
  page.backend.emit("run-meta", {
    reqID: "initial", sourceLang: "en", targetLang: "zh-HANS", services: ["google", "baidu"],
  });
  update("baidu", "", "initial", "offline");
  await tick();
  button("重试").click();
  const retry = lastRun();
  update("google", "original peer completed");
  update("baidu", "retry completed", retry.reqID);
  await tick();
  expect(cardTexts()).toEqual(["original peer completed", "retry completed"]);
});

test("peer retry failures and unavailable metadata settle their own cards", async () => {
  const failed = deferred<null>();
  page.backend.override("TranslateSvc.Run", (req) =>
    (req as RunReq).service === "google" ? failed.promise : null
  );
  await openBubble();
  update("google", "", "initial", "offline");
  update("baidu", "", "initial", "offline");
  await tick();
  const [googleRetry, baiduRetry] = page.target.querySelectorAll<HTMLButtonElement>(".card-error-actions button:first-child");
  googleRetry!.click();
  const first = lastRun();
  baiduRetry!.click();
  const second = lastRun();
  failed.reject(new Error("IPC failed"));
  await settle();
  expect(page.target.querySelector(".card-error")?.textContent).toContain("IPC failed");

  update("baidu", "peer done", second.reqID);
  await tick();
  expect(page.target.querySelector(".card-loading")).toBeNull();
  button("重试").click();
  const third = lastRun();
  update("baidu", "", second.reqID, "offline");
  await tick();
  button("重试").click();
  page.backend.emit("run-meta", { reqID: third.reqID, services: ["baidu"] });
  update("google", "stale", first.reqID);
  await tick();
  expect(page.target.querySelector(".card-error")?.textContent).toContain("该服务当前不可用");
  expect(cardTexts()).not.toContain("stale");
});

test("English Ready snapshot renders copy, speech and naming controls", async () => {
  page.backend.bubble.targetLang = "en";
  page.backend.bubble.results = [{
    reqID: "initial", service: "google", text: "user name", detectedLang: "", err: "", streaming: false,
  }];
  await openBubble();
  const card = page.target.querySelector(".card-ok")!;
  expect(button("朗读", card)).toBeDefined();
  expect(button("复制", card)).toBeDefined();
  button("小驼峰：userName", card).click();
  expect(page.backend.calls("BubbleSvc.Copy")).toEqual([["userName"]]);
  await vi.waitFor(() => expect(page.target.querySelector(".toast-host")?.textContent).toContain("已复制 小驼峰"));
});

test("copy waits for IPC and reports clipboard failures", async () => {
  const copy = deferred<void>();
  page.backend.override("BubbleSvc.Copy", () => copy.promise);
  await openBubble();
  button("复制输入").click();
  await tick();
  const toasts = page.target.querySelector(".toast-host")!;
  expect(toasts.textContent).not.toContain("已复制");
  copy.reject(new Error("复制失败"));
  await settle();
  expect(toasts.textContent).toContain("复制失败");
  expect(toasts.textContent).not.toContain("已复制");
});

test("focusing the same presentation retains draft, languages, cards and the active request", async () => {
  await openBubble();
  const source = page.target.querySelector<HTMLSelectElement>(".source-select")!;
  source.value = "fr";
  source.dispatchEvent(new Event("change", { bubbles: true }));
  const target = page.target.querySelector<HTMLSelectElement>(".lang-select")!;
  target.value = "en";
  target.dispatchEvent(new Event("change", { bubbles: true }));
  const request = lastRun();
  update("google", "current result", request.reqID);
  await settle();
  const composer = input();
  const card = page.target.querySelector(".card-ok");
  edit("unsent draft");
  button("复制", card!).focus();
  const runs = page.backend.calls("TranslateSvc.Run").length;

  present(1, request.revision);
  await tick();
  expect(input()).toBe(composer);
  expect(input().value).toBe("unsent draft");
  expect(document.activeElement).toBe(composer);
  expect(source.value).toBe("fr");
  expect(target.value).toBe("en");
  expect(page.target.querySelector(".card-ok")).toBe(card);
  expect(page.backend.calls("TranslateSvc.Run")).toHaveLength(runs);
  update("baidu", "peer completed", request.reqID);
  await tick();
  expect(cardTexts()).toEqual(["current result", "peer completed"]);
});

test("close uses the newest presentation and waits for acceptance before stopping", async () => {
  const close = deferred<boolean>();
  page.backend.override("BubbleSvc.Close", () => close.promise);
  await openBubble();
  edit("unsent draft");
  present(2);
  present(1);
  await tick();
  expect(input().value).toBe("unsent draft");
  expect(document.activeElement).toBe(input());
  button("关闭").click();
  await tick();
  expect(page.backend.calls("TranslateSvc.Cancel")).toEqual([]);
  expect(page.backend.calls("BubbleSvc.Close")).toEqual([[1, "test-session", 2, 2]]);
  expect(page.target.querySelector(".card-loading")).not.toBeNull();
  stream("result while closing");
  await tick();
  expect(cardTexts()).toEqual(["result while closing"]);

  close.resolve(true);
  await settle();
  expect(input().value).toBe("unsent draft");
  expect(page.target.querySelector(".card-loading")).toBeNull();
  expect(page.target.querySelector(".card-streaming")).toBeNull();
  expect(page.target.querySelector(".card-error")?.textContent).toContain("翻译已取消");
  update("google", "late result");
  await tick();
  expect(cardTexts()).toEqual(["result while closing"]);
  expect(run("resume").revision).toBe(3);
});

test.each([
  ["refused", 1], ["refused", 2], ["failed", 1], ["failed", 2],
] as const)("a %s close for show %s retains draft, languages, cards and the active request", async (outcome, show) => {
  const close = deferred<boolean>();
  page.backend.override("BubbleSvc.Close", () => close.promise);
  await openBubble();
  const source = page.target.querySelector<HTMLSelectElement>(".source-select")!;
  source.value = "fr";
  source.dispatchEvent(new Event("change", { bubbles: true }));
  const target = page.target.querySelector<HTMLSelectElement>(".lang-select")!;
  target.value = "en";
  target.dispatchEvent(new Event("change", { bubbles: true }));
  const request = lastRun();
  update("google", "current result", request.reqID);
  await settle();
  const card = page.target.querySelector(".card-ok");
  edit("unsent draft");
  button("关闭").click();
  expect(page.backend.calls("BubbleSvc.Close")).toEqual([[1, "test-session", 1, request.revision + 1]]);
  present(show, request.revision);
  await tick();
  expect(page.target.querySelector(".card-loading")).not.toBeNull();
  if (outcome === "failed") close.reject(new Error("close IPC failed"));
  else close.resolve(false);
  await settle();

  expect(input().value).toBe("unsent draft");
  expect(source.value).toBe("fr");
  expect(target.value).toBe("en");
  expect(page.target.querySelector(".card-ok")).toBe(card);
  expect(page.target.querySelector(".card-loading")).not.toBeNull();
  expect(page.target.querySelector(".toast-host")?.textContent ?? "").toBe(outcome === "failed" ? "Error: close IPC failed" : "");
  update("baidu", "peer completed after close", request.reqID);
  await settle();
  expect(cardTexts()).toEqual(["current result", "peer completed after close"]);
  expect(page.backend.calls("TranslateSvc.Run")).toHaveLength(2);
  expect(run("resume").revision).toBe(request.revision + 2);
});

// These confirmation-only tests exercise the promise guard, not the backend's accepted-close stop event.
test("a Run while Close is pending uses a higher revision and ignores the late close confirmation alone", async () => {
  const close = deferred<boolean>();
  page.backend.override("BubbleSvc.Close", () => close.promise);
  await openBubble();
  button("关闭").click();
  const closingRevision = page.backend.calls("BubbleSvc.Close")[0]![3] as number;
  const request = run("new request");
  expect(request.revision).toBe(closingRevision + 1);
  await tick();
  expect(page.target.querySelectorAll(".card-loading")).toHaveLength(2);

  close.resolve(true);
  await settle();
  expect(input().value).toBe("new request");
  expect(page.target.querySelectorAll(".card-loading")).toHaveLength(2);
  expect(page.target.querySelector(".card-error")).toBeNull();
  update("google", "new result", request.reqID);
  update("baidu", "new peer result", request.reqID);
  await tick();
  expect(cardTexts()).toEqual(["new result", "new peer result"]);
  expect(run("resume").revision).toBe(request.revision + 1);
});

test.each(["show", "session", "revision"] as const)("late close confirmation alone cannot stop a newer %s", async (identity) => {
  const close = deferred<boolean>();
  page.backend.override("BubbleSvc.Close", () => close.promise);
  await openBubble();
  button("关闭").click();
  let requestID = "initial";
  let currentRevision = identity === "show" ? 2 : 1;
  if (identity === "show") present(2);
  else if (identity === "session") {
    page.backend.emit("bubble-reset", {
      ...structuredClone(page.backend.bubble), sessionID: "next-session", generation: 2, reqID: "next-request",
    });
    requestID = "next-request";
  } else {
    run("new request");
    const request = run("newest request");
    requestID = request.reqID;
    currentRevision = request.revision;
  }
  edit("new draft");
  close.resolve(true);
  await settle();
  expect(input().value).toBe("new draft");
  expect(page.target.querySelector(".card-loading")).not.toBeNull();
  update("google", "current request completed", requestID);
  await tick();
  expect(cardTexts()).toEqual(["current request completed"]);
  expect(run("resume").revision).toBe(currentRevision + 1);
});

test.each(["confirmation", "event"] as const)("an accepted close stops a Run submitted while closing with %s first", async (first) => {
  const close = deferred<boolean>();
  page.backend.override("BubbleSvc.Close", () => close.promise);
  await openBubble();
  button("关闭").click();
  const [bubbleID, sessionID, showID, closingRevision] = page.backend.calls("BubbleSvc.Close")[0]! as [number, string, number, number];
  expect(bubbleID).toBe(1);
  const request = run("request submitted while closing");
  expect(page.backend.calls("TranslateSvc.Run")).toHaveLength(1);
  expect(request.sessionID).toBe(sessionID);
  expect(request.revision).toBeGreaterThan(closingRevision);
  expect(request.revision).toBe(closingRevision + 1);
  await tick();
  expect(page.target.querySelectorAll(".card-loading")).toHaveLength(2);

  // OnClose advances the accepted revision; the backend refuses this same-show Run.
  const stopped = { sessionID, showID, revision: closingRevision + 1 };
  if (first === "confirmation") close.resolve(true);
  else page.backend.emit("run-stopped", stopped);
  await settle();
  expect(page.target.querySelectorAll(".card-loading")).toHaveLength(first === "confirmation" ? 2 : 0);
  expect(page.target.querySelectorAll(".card.card-error")).toHaveLength(first === "confirmation" ? 0 : 2);

  if (first === "confirmation") page.backend.emit("run-stopped", stopped);
  else close.resolve(true);
  await settle();
  expect(input().value).toBe("request submitted while closing");
  expect(page.target.querySelector(".card-loading")).toBeNull();
  expect(page.target.querySelector(".card-streaming")).toBeNull();
  const cancelled = [...page.target.querySelectorAll(".card.card-error")];
  expect(cancelled).toHaveLength(2);
  for (const card of cancelled) expect(card.textContent).toContain("翻译已取消");

  update("google", "late result after accepted close", request.reqID);
  update("baidu", "late peer result after accepted close", request.reqID);
  await tick();
  expect(page.target.querySelector(".card-ok")).toBeNull();
  expect(page.target.textContent).not.toContain("late result after accepted close");
  expect(page.target.textContent).not.toContain("late peer result after accepted close");
  expect([...page.target.querySelectorAll(".card.card-error")]).toEqual(cancelled);
  expect(run("resume").revision).toBe(stopped.revision + 1);
});

test.each(["confirmation", "event"] as const)("accepted close and RunStopped are safe with %s first", async (first) => {
  const close = deferred<boolean>();
  page.backend.override("BubbleSvc.Close", () => close.promise);
  await openBubble();
  stream("partial result");
  await tick();
  button("关闭").click();
  await tick();
  expect(page.target.querySelector(".card-streaming")).not.toBeNull();
  if (first === "confirmation") close.resolve(true);
  else page.backend.emit("run-stopped", { sessionID: "test-session", showID: 1, revision: 5 });
  await settle();
  expect(page.target.querySelector(".card-streaming")).toBeNull();
  expect(page.target.querySelector(".card-error")?.textContent).toContain("翻译已取消");
  if (first === "confirmation") page.backend.emit("run-stopped", { sessionID: "test-session", showID: 1, revision: 5 });
  else close.resolve(true);
  await settle();
  update("google", "late result");
  await tick();
  expect(cardTexts()).toEqual(["partial result"]);
  expect(run("resume").revision).toBe(6);
});

test("a stop from an earlier presentation cannot cancel a reopened panel's run", async () => {
  await openBubble();
  present(2);
  const request = run("new");
  page.backend.emit("run-stopped", { sessionID: "test-session", showID: 1, revision: 99 });
  update("google", "new result", request.reqID);
  await tick();
  expect(cardTexts()).toEqual(["new result"]);
});

test("presentation reconciles a native stop without erasing the draft", async () => {
  await openBubble();
  edit("keep draft");
  present(2, 3);
  await tick();
  expect(input().value).toBe("keep draft");
  expect(page.target.querySelector(".card-loading")).toBeNull();
  keydown("Enter", input());
  expect(lastRun().revision).toBe(4);
});

test("clipboard recovery remains available after an error and can be retried", async () => {
  let fail = true;
  page.backend.override("BubbleSvc.RestoreClipboard", () => {
    if (fail) throw new Error("clipboard busy");
  });
  await openBubble();
  present(2, 1, "backup");
  await tick();
  const restore = button("重试恢复剪贴板");
  restore.click();
  await settle();
  expect(page.target.querySelector(".toast-host")?.textContent).toContain("clipboard busy");
  expect(restore.disabled).toBe(false);
  expect(button("放弃恢复，继续取词")).toBeDefined();

  fail = false;
  restore.click();
  await settle();
  expect(page.target.contains(restore)).toBe(false);
  expect(page.target.querySelector(".toast-host")?.textContent).toContain("剪贴板已恢复");
  expect(page.backend.calls("BubbleSvc.RestoreClipboard")).toEqual([["backup"], ["backup"]]);
});

test("unmount releases bubble subscriptions and remount adds only one Escape handler", async () => {
  await openBubble();
  await page.destroy();
  expect([...page.backend.events.values()].every((listeners) => listeners.size === 0)).toBe(true);
  keydown("Escape");
  expect(page.backend.calls("BubbleSvc.Close")).toEqual([]);
  await openBubble();
  keydown("Escape");
  expect(page.backend.calls("BubbleSvc.Close")).toHaveLength(1);
});

test.each(["resolve", "reject"] as const)("late Ready %s does not change a new mount", async (outcome) => {
  const ready = deferred<BubbleState>();
  const call = page.backend.call.getMockImplementation()!;
  page.backend.call.mockImplementationOnce((method, ...args) =>
    method === "BubbleSvc.Ready" ? ready.promise : call(method, ...args)
  );
  page.track(mount(BubblePage, { target: page.target, props: { id: 1 } }));
  await tick();
  expect(page.backend.calls("BubbleSvc.Ready")).toHaveLength(1);
  await page.destroy();

  page.backend.bubble.text = "new mount";
  await openBubble();
  if (outcome === "resolve") ready.resolve({ ...page.backend.bubble, text: "stale mount" });
  else ready.reject(new Error("old page closed"));
  await settle();
  expect(input().value).toBe("new mount");
  expect(page.target.querySelector(".bubble-error")).toBeNull();
});

test("naming format resolution and peer updates retain focused controls", async () => {
  const formats = deferred<FormatOption[]>();
  page.backend.override("TranslateSvc.Formats", (text) => text === "hello world" ? formats.promise : []);
  page.backend.bubble.targetLang = "en";
  await openBubble();
  update("google", "hello world");
  await tick();
  const card = page.target.querySelector(".card-ok")!;
  const copy = button("复制", card);
  copy.focus();
  formats.resolve([{ style: "snake", label: "下划线", value: "hello_world" }]);
  await settle();
  expect(document.activeElement).toBe(copy);
  expect(page.target.querySelector(".card-ok")).toBe(card);

  const format = button("下划线：hello_world");
  format.focus();
  update("baidu", "peer result");
  await settle();
  expect(button("下划线：hello_world")).toBe(format);
  expect(document.activeElement).toBe(format);
});

test("display height changes preserve the translation, scroll and focus", async () => {
  page.backend.bubble.maxHeight = 540;
  await openBubble();
  update("google", "一段完整的长译文。".repeat(100));
  await tick();
  const bubble = page.target.querySelector<HTMLElement>(".bubble")!;
  expect(bubble.style.getPropertyValue("--bubble-max-height")).toBe("540px");
  const card = page.target.querySelector(".card-ok")!;
  const scroll = page.target.querySelector<HTMLElement>(".bubble-scroll")!;
  scroll.scrollTop = 80;
  button("复制", card).focus();
  page.backend.emit("bubble-layout-changed", 480);
  await tick();
  expect(bubble.style.getPropertyValue("--bubble-max-height")).toBe("480px");
  expect(page.target.querySelector(".card-ok")).toBe(card);
  expect(scroll.scrollTop).toBe(80);
  expect(document.activeElement).toBe(button("复制", card));
});

test("a full translation starts at the top, while retry retains the reading position", async () => {
  await openBubble();
  update("google", "你好");
  update("baidu", "", "initial", "网络错误");
  await tick();
  const scroll = page.target.querySelector<HTMLElement>(".bubble-scroll")!;
  scroll.scrollTop = 100;
  button("重试").click();
  await tick();
  expect(scroll.scrollTop).toBe(100);
  run("A new paragraph");
  await tick();
  expect(scroll.scrollTop).toBe(0);
});

function stream(text: string, reqID = "initial"): void {
  page.backend.emit("translation-update", {
    reqID, service: "google", text, err: "", detectedLang: "en", streaming: true,
  });
}

test("streaming text appears immediately and actions enable only after completion", async () => {
  page.backend.bubble.targetLang = "en";
  await openBubble();
  stream("hello");
  await tick();
  const card = page.target.querySelector(".card-streaming")!;
  const text = card.querySelector(".card-text");
  const copy = button("复制", card);
  expect(text?.textContent).toBe("hello");
  expect(card.querySelector(".skeleton")).toBeNull();
  expect(card.querySelector(".card-status")?.textContent).toBe("生成中");
  expect(card.querySelector(".card-body")?.getAttribute("aria-busy")).toBe("true");
  expect(copy.disabled).toBe(true);
  expect(button("朗读", card).disabled).toBe(true);
  copy.click();
  expect(page.backend.calls("BubbleSvc.Copy")).toEqual([]);
  expect(page.backend.calls("TranslateSvc.Formats")).toEqual([]);

  card.querySelector<HTMLButtonElement>(".card-toggle")!.focus();
  const focused = document.activeElement;
  const scroll = page.target.querySelector<HTMLElement>(".bubble-scroll")!;
  scroll.scrollTop = 80;
  stream("hello world");
  await tick();
  expect(card.querySelector(".card-text")).toBe(text);
  expect(text?.textContent).toBe("hello world");
  expect(scroll.scrollTop).toBe(80);
  expect(document.activeElement).toBe(focused);
  update("google", "hello world");
  await settle();
  expect(page.target.querySelector(".card-ok")).toBe(card);
  expect(button("复制", card)).toBe(copy);
  expect(copy.disabled).toBe(false);
  expect(button("朗读", card).disabled).toBe(false);
  expect(card.querySelector(".card-status")?.textContent).toBe("");
  expect(page.backend.calls("TranslateSvc.Formats")).toEqual([["hello world"]]);
  copy.click();
  expect(page.backend.calls("BubbleSvc.Copy")).toEqual([["hello world"]]);
});

test("interrupted text is retained with retry and no copy action", async () => {
  await openBubble();
  stream("部分译文");
  update("google", "部分译文", "initial", "连接中断，请重试");
  await tick();
  const card = page.target.querySelector(".card-error")!.closest(".card")!;
  expect(card.querySelector(".card-text")?.textContent).toBe("部分译文");
  expect(card.querySelector(".card-error")?.textContent).toContain("译文未完成");
  expect(button("复制", card).disabled).toBe(true);
  button("重试", card).click();
  const retry = lastRun();
  stream("迟到的旧结果");
  await tick();
  expect(cardTexts()).toEqual([]);
  stream("新的译文", retry.reqID);
  await tick();
  expect(cardTexts()).toEqual(["新的译文"]);
  update("google", "新的完整译文", retry.reqID);
  await tick();
  expect(button("复制", card).disabled).toBe(false);
});

test("Ready restores in-progress text and a native stop retains it", async () => {
  page.backend.bubble.results = [{
    reqID: "initial", service: "google", text: "恢复中的译文", err: "", detectedLang: "en", streaming: true,
  }];
  await openBubble();
  expect(cardTexts()).toEqual(["恢复中的译文"]);
  expect(page.target.querySelector(".card-streaming")).not.toBeNull();
  page.backend.emit("run-stopped", { sessionID: "test-session", showID: 1, revision: 3 });
  await tick();
  expect(cardTexts()).toEqual(["恢复中的译文"]);
  expect(page.target.querySelector(".card-streaming")).toBeNull();
  expect(page.target.querySelector(".card-error")?.textContent).toContain("翻译已取消");
  stream("取消后的旧结果");
  await tick();
  expect(cardTexts()).toEqual(["恢复中的译文"]);
});

test("appending streamed text preserves earlier spans and the reader's selection", async () => {
  await openBubble();
  stream("你好");
  await tick();
  const text = page.target.querySelector(".card-text")!;
  const first = text.querySelector("span")!;
  const selection = window.getSelection()!;
  const range = document.createRange();
  range.selectNodeContents(first);
  selection.removeAllRanges();
  selection.addRange(range);

  stream("你好，世界");
  await tick();
  expect(text.textContent).toBe("你好，世界");
  expect(text.querySelector("span")).toBe(first);
  expect(selection.toString()).toBe("你好");
  update("google", "你好，世界。");
  await tick();
  expect(text.textContent).toBe("你好，世界。");
  expect(text.querySelector("span")).toBe(first);
  expect(selection.toString()).toBe("你好");
  selection.removeAllRanges();
});

test("a provider correction replaces the old text without duplicating it", async () => {
  await openBubble();
  stream("原来的文字");
  await tick();
  stream("修正后的文字");
  await tick();
  expect(cardTexts()).toEqual(["修正后的文字"]);
  update("google", "最终译文");
  await tick();
  expect(cardTexts()).toEqual(["最终译文"]);
});

test("new input clears old draft and results before acknowledging measured layout", async () => {
  const measure = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({ height: 218.5 } as DOMRect);
  try {
    await openBubble();
    edit("unsent old draft");
    page.backend.emit("translation-update", { reqID: "initial", service: "google", text: "old translation", streaming: false, err: "", detectedLang: "en" });
    await settle();
    const st = { ...structuredClone(page.backend.bubble), trigger: "input", text: "", sessionID: "fresh", reqID: "fresh", generation: 2, showID: 2, layoutID: 2, revision: -1, results: [], serviceRequests: {} };
    page.backend.emit("bubble-reset", st);
    // No acknowledgement until Svelte has committed the replacement DOM.
    expect(page.backend.calls("BubbleSvc.Rendered").some((args) => args[1] === "fresh")).toBe(false);
    await settle();
    expect(input().value).toBe("");
    expect(page.target.textContent).not.toContain("old translation");
    expect(page.backend.calls("BubbleSvc.Rendered").at(-1)).toEqual([1, "fresh", 2, 2, expect.any(Number), 218.5]);
  } finally {
    measure.mockRestore();
  }
});

test("only the latest OCR layout is acknowledged when completion supersedes recognition", async () => {
  const measure = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({ height: 240 } as DOMRect);
  try {
    await openBubble();
    const st = { ...structuredClone(page.backend.bubble), trigger: "ocr", text: "", sessionID: "ocr", reqID: "ocr", generation: 2, showID: 2, layoutID: 2, revision: -1, phase: "recognizing" };
    page.backend.emit("bubble-reset", st);
    page.backend.emit("bubble-reset", { ...st, layoutID: 3, text: "recognized text", phase: "" });
    await settle();
    expect(input().value).toBe("recognized text");
    const acks = page.backend.calls("BubbleSvc.Rendered").filter((args) => args[1] === "ocr");
    expect(acks).toEqual([[1, "ocr", 2, 3, expect.any(Number), 240]]);
  } finally {
    measure.mockRestore();
  }
});


test("late recognition snapshot cannot replace completed OCR content or its layout", async () => {
  await openBubble();
  const st = { ...structuredClone(page.backend.bubble), trigger: "ocr", sessionID: "ocr", reqID: "ocr", generation: 2, showID: 2, layoutID: 3, revision: -1, text: "recognized text", phase: "" };
  page.backend.emit("bubble-reset", st);
  await settle();
  page.backend.emit("bubble-reset", { ...st, layoutID: 2, text: "", phase: "recognizing" });
  await settle();
  expect(input().value).toBe("recognized text");
  expect(page.target.textContent).not.toContain("正在识别文字");
});

test("one observer reports growth, folding and resize in order and disconnects on unmount", async () => {
  let notify!: () => void;
  const observe = vi.fn();
  const disconnect = vi.fn();
  vi.stubGlobal("ResizeObserver", class {
    constructor(callback: ResizeObserverCallback) {
      notify = () => callback([], this as unknown as ResizeObserver);
    }
    observe = observe;
    disconnect = disconnect;
  });
  let height = 200;
  let width = 480;
  const measure = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(() => ({ width, height }) as DOMRect);
  try {
    await openBubble();
    expect(observe).toHaveBeenCalledWith(page.target.querySelector(".bubble"));
    const firstSequence = page.backend.calls("BubbleSvc.Rendered")[0]![4] as number;
    expect(page.backend.calls("BubbleSvc.Rendered").at(-1)).toEqual([1, "test-session", 1, 1, firstSequence, 200]);
    notify(); // unchanged dimensions are deduplicated
    expect(page.backend.calls("BubbleSvc.Rendered")).toHaveLength(1);

    update("google", "a longer translation");
    await tick();
    height = 560;
    notify();
    expect(page.backend.calls("BubbleSvc.Rendered").at(-1)).toEqual([1, "test-session", 1, 1, firstSequence + 1, 560]);
    height = 200; // a newer card fold is allowed to shrink the window
    notify();
    expect(page.backend.calls("BubbleSvc.Rendered").at(-1)).toEqual([1, "test-session", 1, 1, firstSequence + 2, 200]);
    width = 384; // page zoom can change width without changing measured height
    window.dispatchEvent(new Event("resize"));
    expect(page.backend.calls("BubbleSvc.Rendered").at(-1)).toEqual([1, "test-session", 1, 1, firstSequence + 3, 200]);

    await page.destroy();
    expect(disconnect).toHaveBeenCalledOnce();
    height = 760;
    notify();
    window.dispatchEvent(new Event("resize"));
    expect(page.backend.calls("BubbleSvc.Rendered")).toHaveLength(4);
  } finally {
    measure.mockRestore();
  }
});

test("layout sequence resumes above Ready floor and survives remount with old IPC still in flight", async () => {
  const measure = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({ width: 480, height: 240 } as DOMRect);
  try {
    page.backend.bubble.layoutSequence = 1_000_000;
    await openBubble();
    const first = page.backend.calls("BubbleSvc.Rendered").at(-1)!;
    expect(first[4]).toBeGreaterThan(page.backend.bubble.layoutSequence);
    await page.destroy();
    // Ready may not yet include the preceding mount's in-flight measurement.
    await openBubble();
    const next = page.backend.calls("BubbleSvc.Rendered").at(-1)!;
    expect(next[4]).toBeGreaterThan(first[4] as number);
  } finally {
    measure.mockRestore();
  }
});

// Keep timers deterministic without letting openBubble's polling consume retry delays.
async function withMeasuredLayout(check: (resize: (height: number) => void) => Promise<void>): Promise<void> {
  let height = 200;
  const measure = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect")
    .mockImplementation(() => ({ width: 480, height }) as DOMRect);
  vi.useFakeTimers();
  try {
    page.track(mount(BubblePage, { target: page.target, props: { id: 1 } }));
    await tick();
    await vi.advanceTimersByTimeAsync(0);
    await check((next) => { height = next; });
  } finally {
    await page.destroy();
    vi.useRealTimers();
    measure.mockRestore();
  }
}

function layoutCalls(): unknown[][] {
  return page.backend.calls("BubbleSvc.Rendered");
}

function expectIncreasingLayoutSequences(): void {
  const sequences = layoutCalls().map((args) => args[4] as number);
  for (let i = 1; i < sequences.length; i++) expect(sequences[i]).toBeGreaterThan(sequences[i - 1]!);
}

test("a rejected layout retries the unchanged DOM without an observer notification", async () => {
  let attempts = 0;
  page.backend.override("BubbleSvc.Rendered", () => {
    if (++attempts === 1) throw new Error("temporary IPC failure");
    return true;
  });
  await withMeasuredLayout(async () => {
    expect(layoutCalls()).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(50);
    expect(layoutCalls()).toHaveLength(2);
    expect(layoutCalls().map((args) => args[5])).toEqual([200, 200]);
    expectIncreasingLayoutSequences();
    window.dispatchEvent(new Event("resize"));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls()).toHaveLength(2);
    expect(vi.getTimerCount()).toBe(0);
  });
});

test.each(["false", "reject", "pending"] as const)("a permanently %s layout stops after three retries", async (outcome) => {
  page.backend.override("BubbleSvc.Rendered", () => {
    if (outcome === "reject") throw new Error("closed window");
    if (outcome === "pending") return new Promise<boolean>(() => {});
    return false;
  });
  await withMeasuredLayout(async () => {
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls()).toHaveLength(4);
    expect(layoutCalls().map((args) => args[5])).toEqual([200, 200, 200, 200]);
    expectIncreasingLayoutSequences();
    expect(vi.getTimerCount()).toBe(0);
    for (let i = 0; i < 5; i++) window.dispatchEvent(new Event("resize"));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls()).toHaveLength(4);
  });
});

test.each(["resolve", "reject"] as const)("a timed-out attempt's late %s cannot settle its pending retry", async (outcome) => {
  const first = deferred<boolean>();
  const second = deferred<boolean>();
  let attempts = 0;
  page.backend.override("BubbleSvc.Rendered", () => {
    attempts++;
    return attempts === 1 ? first.promise : attempts === 2 ? second.promise : true;
  });
  await withMeasuredLayout(async () => {
    await vi.advanceTimersByTimeAsync(1050);
    expect(layoutCalls()).toHaveLength(2);
    if (outcome === "reject") first.reject(new Error("late timed-out failure"));
    else first.resolve(true);
    await vi.advanceTimersByTimeAsync(1100);
    expect(layoutCalls()).toHaveLength(3);
    second.resolve(false);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls()).toHaveLength(3);
    expectIncreasingLayoutSequences();
    expect(vi.getTimerCount()).toBe(0);
  });
});

test("a retry measures live dimensions even before the observer delivers them", async () => {
  let attempts = 0;
  page.backend.override("BubbleSvc.Rendered", () => ++attempts > 1);
  await withMeasuredLayout(async (resize) => {
    resize(460);
    await vi.advanceTimersByTimeAsync(50);
    expect(layoutCalls().map((args) => args[5])).toEqual([200, 460]);
    expectIncreasingLayoutSequences();
    window.dispatchEvent(new Event("resize"));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls()).toHaveLength(2);
  });
});

test.each(["resolve", "reject", "false"] as const)("old layout %s cannot cancel or retry over a newer measurement", async (outcome) => {
  const old = deferred<boolean>();
  let attempts = 0;
  page.backend.override("BubbleSvc.Rendered", () => {
    if (++attempts === 1) return old.promise;
    return attempts > 2;
  });
  await withMeasuredLayout(async (resize) => {
    resize(480);
    window.dispatchEvent(new Event("resize"));
    await vi.advanceTimersByTimeAsync(0);
    if (outcome === "reject") old.reject(new Error("late old IPC failure"));
    else old.resolve(outcome === "resolve");
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls().map((args) => args[5])).toEqual([200, 480, 480]);
    expectIncreasingLayoutSequences();
    window.dispatchEvent(new Event("resize"));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls()).toHaveLength(3);
  });
});

test.each(["resolve", "reject"] as const)("old layout %s preserves a newer successful measurement's deduplication", async (outcome) => {
  const old = deferred<boolean>();
  let attempts = 0;
  page.backend.override("BubbleSvc.Rendered", () => ++attempts === 1 ? old.promise : true);
  await withMeasuredLayout(async (resize) => {
    resize(480);
    window.dispatchEvent(new Event("resize"));
    await vi.advanceTimersByTimeAsync(0);
    if (outcome === "reject") old.reject(new Error("late old IPC failure"));
    else old.resolve(true);
    await vi.advanceTimersByTimeAsync(10_000);
    window.dispatchEvent(new Event("resize"));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls().map((args) => args[5])).toEqual([200, 480]);
  });
});

test.each(["session", "show", "layout"] as const)("a new %s cancels the previous layout retry at unchanged size", async (identity) => {
  page.backend.override("BubbleSvc.Rendered", (...args) => {
    if (identity === "session") return args[1] === "next-session";
    if (identity === "show") return args[2] === 2;
    return args[3] === 2;
  });
  await withMeasuredLayout(async () => {
    if (identity === "show") present(2);
    else page.backend.emit("bubble-reset", {
      ...structuredClone(page.backend.bubble),
      ...(identity === "session" ? { sessionID: "next-session", generation: 2 } : { layoutID: 2 }),
    });
    await tick();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls()).toHaveLength(2);
    expect(layoutCalls()[1]!.slice(1, 4)).toEqual([
      identity === "session" ? "next-session" : "test-session",
      identity === "show" ? 2 : 1,
      identity === "layout" ? 2 : 1,
    ]);
    expectIncreasingLayoutSequences();
    expect(vi.getTimerCount()).toBe(0);
  });
});

test.each(["retry", "pending"] as const)("unmount clears the layout %s timer and ignores late completion", async (stage) => {
  const pending = deferred<boolean>();
  page.backend.override("BubbleSvc.Rendered", () => stage === "retry" ? false : pending.promise);
  await withMeasuredLayout(async () => {
    expect(vi.getTimerCount()).toBe(1);
    await page.destroy();
    expect(vi.getTimerCount()).toBe(0);
    if (stage === "pending") pending.reject(new Error("unmounted"));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(layoutCalls()).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(0);
  });
});

test("recovering from Ready failure observes the replacement bubble root", async () => {
  const observers: { observe: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn>; notify: () => void }[] = [];
  vi.stubGlobal("ResizeObserver", class {
    observe = vi.fn();
    disconnect = vi.fn();
    constructor(callback: ResizeObserverCallback) {
      observers.push({ observe: this.observe, disconnect: this.disconnect, notify: () => callback([], this as unknown as ResizeObserver) });
    }
  });
  let height = 200;
  const measure = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(() => ({ width: 480, height }) as DOMRect);
  try {
    page.backend.override("BubbleSvc.Ready", () => { throw new Error("temporary failure"); });
    page.track(mount(BubblePage, { target: page.target, props: { id: 1 } }));
    await vi.waitFor(() => expect(page.target.querySelector(".bubble-error")).not.toBeNull());
    expect(observers[0]!.disconnect).toHaveBeenCalledOnce();
    page.backend.emit("bubble-reset", structuredClone(page.backend.bubble));
    await settle();
    expect(observers).toHaveLength(2);
    expect(observers[1]!.observe).toHaveBeenCalledWith(page.target.querySelector(".bubble"));
    height = 500;
    observers[1]!.notify();
    expect(page.backend.calls("BubbleSvc.Rendered").at(-1)?.at(-1)).toBe(500);
  } finally {
    measure.mockRestore();
  }
});
