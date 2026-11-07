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

test("close uses the newest presentation and preserves the draft until closing", async () => {
  await openBubble();
  edit("unsent draft");
  present(2);
  present(1);
  await tick();
  expect(input().value).toBe("unsent draft");
  expect(document.activeElement).toBe(input());
  button("关闭").click();
  expect(page.backend.calls("TranslateSvc.Cancel")).toEqual([]);
  expect(page.backend.calls("BubbleSvc.Close")).toEqual([[1, "test-session", 2, 2]]);
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
