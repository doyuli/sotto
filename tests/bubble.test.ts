import { beforeEach, expect, test } from "bun:test";
import { renderBubble } from "../src/pages/bubble";
import { renderSnip } from "../src/pages/snip";
import type { BubbleState, RunReq } from "../src/mygo";
import { Element, TestDocument } from "./dom";

let doc: TestDocument, root: Element, calls: {method:string;args:any[]}[];
let listeners: Map<string, Set<(message:any)=>void>>;
let initial: BubbleState;
let callOverride: ((method:string, args:any[])=>unknown) | undefined;
function emit(name:string, message:any) { for (const handler of listeners.get(name) ?? []) handler(message); }
function button(text:string) {
 const found = root.querySelectorAll("button").find(n=>n.textContent===text || n.title===text);
 if (!found) throw new Error(`missing button ${text}`);
 return found;
}
function input() { return root.querySelector("textarea")!; }
function currentRequest(): RunReq { return calls.filter(c=>c.method==="TranslateSvc.Run").at(-1)!.args[0]; }
beforeEach(async () => {
 doc=new TestDocument(); root=doc.createElement("div"); doc.body.append(root);
 calls=[]; listeners=new Map(); callOverride=undefined;
 initial={id:1,showID:1,clipboardRecoveryID:"",serviceRequests:{google:"r",baidu:"r"},sessionID:"s",generation:1,revision:0,reqID:"r",trigger:"selection",text:"hello",sourceLang:"auto",targetLang:"zh-HANS",pinned:false,phase:"",message:"",meta:null,results:[],settings:{
  services:{enabled:["google","baidu"],order:["google","baidu"],openai:{baseURL:"",model:""}},
  targetLanguage:"zh-HANS",identifier:{autoSplit:true},copyFormats:["snake"],hotkeys:{panel:"",selection:"",ocr:""},bubble:{autoHide:true},openAtLogin:false,
 }};
 const storage=new Map<string,string>();
 Object.assign(globalThis,{document:doc,location:{hash:"#/bubble?b=1"},sessionStorage:{getItem:(k:string)=>storage.get(k)??null,setItem:(k:string,v:string)=>storage.set(k,v)},mygo:{
  on:(name:string,fn:(m:any)=>void)=>{ if(!listeners.has(name))listeners.set(name,new Set()); listeners.get(name)!.add(fn); return ()=>listeners.get(name)?.delete(fn); },
  call:async (method:string,...args:any[])=>{ calls.push({method,args}); if(callOverride)return callOverride(method,args); if(method==="BubbleSvc.Ready")return structuredClone(initial); if(method==="TranslateSvc.Formats")return []; },
 }});
 await renderBubble(root as unknown as HTMLElement);
});

test("another service result preserves the selected card node and scroll position", () => {
 emit("translation-update",{reqID:"r",service:"google",text:"你好",err:"",detectedLang:"en"});
 const card=root.querySelector(".card-ok")!;
 const scroll=card.querySelector(".card-scroll")!; scroll.scrollTop=80;
 const copy=card.querySelectorAll("button").find(n=>n.title==="复制")!; copy.focus();
 emit("translation-update",{reqID:"r",service:"baidu",text:"您好",err:"",detectedLang:"en"});
 expect(root.querySelector(".card-ok")).toBe(card);
 expect(scroll.scrollTop).toBe(80); expect(doc.activeElement).toBe(copy);
});

test("retry calls only the failed service and keeps successful card", () => {
 emit("translation-update",{reqID:"r",service:"google",text:"你好",err:""});
 const card=root.querySelector(".card-ok");
 emit("translation-update",{reqID:"r",service:"baidu",text:"",err:"网络错误"});
 button("重试").emit("click");
 expect(currentRequest().service).toBe("baidu");
 expect(root.querySelector(".card-ok")).toBe(card);
});

test("clearing captured text cancels work and leaves an editable composer", () => {
 button("清空输入").emit("click");
 expect(input().value).toBe(""); expect(input().readOnly).toBe(false);
 expect(calls.at(-1)?.method).toBe("TranslateSvc.Cancel");
 expect(calls.at(-1)?.args).toEqual(["s",1]);
});

test("late results and capture resets cannot overwrite a manual revision", () => {
 input().value="new text"; input().emit("keydown",{key:"Enter",isComposing:false,shiftKey:false});
 const request=currentRequest(); expect(request.revision).toBe(1);
 emit("bubble-reset",{...initial,revision:-1,text:"late OCR"});
 expect(input().value).toBe("new text");
 emit("translation-update",{reqID:"r",service:"google",text:"old result",err:""});
 expect(root.querySelector(".card-ok")).toBeNull();
 emit("translation-update",{reqID:request.reqID,service:"google",text:"new result",err:""});
 expect(root.querySelector(".card-text")?.textContent).toBe("new result");
});

test("recognition cancel exposes recovery and prevents a late capture reset", () => {
 emit("bubble-reset",{...initial,sessionID:"ocr",generation:2,reqID:"ocr",revision:-1,trigger:"ocr",phase:"recognizing",text:""});
 expect(input().readOnly).toBe(true);
 button("取消识别").emit("click");
 expect(input().readOnly).toBe(false); expect(button("重新框选")).toBeDefined();
 emit("bubble-reset",{...initial,sessionID:"ocr",generation:2,reqID:"ocr",revision:-1,text:"late capture"});
 expect(input().value).toBe("");
});

test("initial snapshot keeps results if an older reset is delivered afterward", () => {
 emit("translation-update",{reqID:"r",service:"google",text:"你好",err:""});
 emit("bubble-reset",{...initial,generation:0,text:"older session"});
 expect(input().value).toBe("hello");
 expect(root.querySelector(".card-text")?.textContent).toBe("你好");
});

test("Escape still cancels screenshot after another key, and remount removes old handlers", () => {
 Object.assign(globalThis,{location:{hash:"#/snip?t=10"},innerWidth:1000,innerHeight:800});
 renderSnip(root as unknown as HTMLElement);
 doc.emit("keydown",{key:"Shift"});
 doc.emit("keydown",{key:"Escape"});
 expect(calls.filter(c=>c.method==="SnipSvc.Done").map(c=>c.args)).toEqual([[10,null]]);
 Object.assign(globalThis,{location:{hash:"#/snip?t=11"}});
 renderSnip(root as unknown as HTMLElement);
 doc.emit("keydown",{key:"Escape"});
 expect(calls.filter(c=>c.method==="SnipSvc.Done").map(c=>c.args)).toEqual([[10,null],[11,null]]);
});

test("native close invalidates pending results and the next run advances the revision", () => {
 emit("run-stopped",{sessionID:"s",showID:1,revision:3});
 expect(root.querySelectorAll(".card-loading")).toHaveLength(0);
 input().value="resume";
 input().emit("keydown",{key:"Enter",isComposing:false,shiftKey:false});
 expect(currentRequest().revision).toBe(4);
 emit("translation-update",{reqID:"r",service:"google",text:"late",err:""});
 expect(root.querySelector(".card-ok")).toBeNull();
});

test("retrying one failure still accepts another service's pending original response", () => {
 emit("run-meta",{reqID:"r",sourceLang:"auto",targetLang:"zh-HANS",services:["google","baidu"]});
 emit("translation-update",{reqID:"r",service:"baidu",text:"",err:"offline"});
 button("重试").emit("click");
 const retry=currentRequest();
 emit("translation-update",{reqID:"r",service:"google",text:"original peer completed",err:""});
 expect(root.querySelectorAll(".card-text").map(n=>n.textContent)).toContain("original peer completed");
 emit("translation-update",{reqID:retry.reqID,service:"baidu",text:"retry completed",err:""});
 expect(root.querySelectorAll(".card-ok")).toHaveLength(2);
});

test("peer retry IPC errors and unavailable metadata settle their own cards", async () => {
 emit("translation-update",{reqID:"r",service:"google",text:"",err:"offline"});
 emit("translation-update",{reqID:"r",service:"baidu",text:"",err:"offline"});
 let rejectFirst!: (reason:Error)=>void;
 callOverride=(method,args)=> {
  if(method==="TranslateSvc.Run" && args[0].service==="google") {
   return new Promise((_,reject)=>{rejectFirst=reject;});
  }
 };
 button("重试").emit("click"); const first=currentRequest();
 button("重试").emit("click"); const second=currentRequest();
 rejectFirst(new Error("IPC failed"));
 await new Promise(resolve=>setTimeout(resolve,0));
 expect(root.querySelectorAll(".card-error").map(n=>n.textContent)).toContain("Error: IPC failed");
 emit("translation-update",{reqID:second.reqID,service:"baidu",text:"peer done",err:""});
 expect(root.querySelectorAll(".card-loading")).toHaveLength(0);
 button("重试").emit("click"); const third=currentRequest();
 // Start another retry so the unavailable metadata no longer matches global reqID.
 emit("translation-update",{reqID:second.reqID,service:"baidu",text:"",err:"offline"});
 button("重试").emit("click");
 emit("run-meta",{reqID:third.reqID,services:["baidu"],sourceLang:"auto",targetLang:"zh-HANS"});
 expect(root.querySelectorAll(".card-error").map(n=>n.textContent)).toContain("该服务当前不可用，请检查设置");
 emit("translation-update",{reqID:first.reqID,service:"google",text:"stale",err:""});
 expect(root.querySelectorAll(".card-text").map(n=>n.textContent)).not.toContain("stale");
});

test("English Ready snapshot restores naming buttons and keeps copy and speech controls", async () => {
 initial.targetLang="en";
 initial.results=[{reqID:"r",service:"google",text:"hello world",err:"",detectedLang:"zh"}];
 callOverride=(method)=> {
  if(method==="BubbleSvc.Ready")return structuredClone(initial);
  if(method==="TranslateSvc.Formats")return [{style:"snake",label:"蛇形",value:"hello_world"}];
 };
 await renderBubble(root as unknown as HTMLElement);
 await new Promise(resolve=>setTimeout(resolve,0));
 expect(button("蛇形：hello_world")).toBeDefined();
 expect(button("复制")).toBeDefined(); expect(button("朗读")).toBeDefined();
 button("蛇形：hello_world").emit("click");
 expect(calls.filter(c=>c.method==="BubbleSvc.Copy").at(-1)?.args).toEqual(["hello_world"]);
});

test("close sends one guarded command and presentation updates preserve unsent drafts", () => {
 input().value="unsent draft";
 emit("bubble-presented",{sessionID:"s",showID:2,revision:0,focus:true,clipboardRecoveryID:""});
 expect(input().value).toBe("unsent draft");
 emit("bubble-presented",{sessionID:"s",showID:1,revision:0,focus:true,clipboardRecoveryID:""});
 button("关闭").emit("click");
 expect(calls.filter(c=>c.method==="TranslateSvc.Cancel")).toHaveLength(0);
 expect(calls.filter(c=>c.method==="BubbleSvc.Close").at(-1)?.args).toEqual([1,"s",2,1]);
});

test("old close events cannot cancel a reopened panel's new translation", () => {
 emit("bubble-presented",{sessionID:"s",showID:2,revision:0,focus:true,clipboardRecoveryID:""});
 input().value="new"; input().emit("keydown",{key:"Enter",isComposing:false,shiftKey:false});
 const request=currentRequest();
 emit("run-stopped",{sessionID:"s",showID:1,revision:99});
 emit("translation-update",{reqID:request.reqID,service:"google",text:"new result",err:""});
 expect(root.querySelector(".card-text")?.textContent).toBe("new result");
});

test("presentation reconciles a native close without erasing the draft", () => {
 input().value="keep draft";
 emit("bubble-presented",{sessionID:"s",showID:2,revision:3,focus:true,clipboardRecoveryID:""});
 expect(input().value).toBe("keep draft");
 expect(root.querySelectorAll(".card-loading")).toHaveLength(0);
 input().emit("keydown",{key:"Enter",isComposing:false,shiftKey:false});
 expect(currentRequest().revision).toBe(4);
});

test("failed clipboard recovery stays available until explicit successful retry", async () => {
 emit("bubble-presented",{sessionID:"s",showID:2,revision:0,focus:true,clipboardRecoveryID:"backup"});
 let fail=true;
 callOverride=(method)=> { if(method==="BubbleSvc.RestoreClipboard" && fail) throw new Error("clipboard changed"); };
 button("重试恢复剪贴板").emit("click");
 await new Promise(resolve=>setTimeout(resolve,0));
 expect(button("重试恢复剪贴板").disabled).toBe(false);
 expect(button("放弃恢复副本")).toBeDefined();
 fail=false; button("重试恢复剪贴板").emit("click");
 await new Promise(resolve=>setTimeout(resolve,0));
 expect(root.querySelectorAll("button").some(b=>b.textContent==="重试恢复剪贴板")).toBe(false);
});
