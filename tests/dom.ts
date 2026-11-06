// Minimal DOM for settings interaction tests; this does not test browser layout.
export class Element {
  children: Element[] = [];
  parent: Element | null = null;
  listeners = new Map<string, Set<(event: any) => void>>();
  attributes = new Map<string, string>();
  className = "";
  textContent = "";
  value = "";
  id = "";
  type = "";
  hidden = false;
  disabled = false;
  checked = false;
  placeholder = "";
  classList = {
    contains: (name: string) => this.className.split(" ").includes(name),
    add: (name: string) => { this.classList.toggle(name, true); },
    remove: (name: string) => { this.classList.toggle(name, false); },
    toggle: (name: string, force = !this.classList.contains(name)) => {
      const names = new Set(this.className.split(" ").filter(Boolean));
      if (force) names.add(name);
      else names.delete(name);
      this.className = [...names].join(" ");
      return force;
    },
  };

  constructor(public tag: string, public ownerDocument?: TestDocument) {}

  append(...children: Element[]) {
    for (const child of children) this.insertBefore(child, null);
  }

  replaceChildren(...children: Element[]) {
    for (const child of this.children) child.parent = null;
    this.children = [];
    this.append(...children);
  }

  insertBefore(child: Element, next: Element | null) {
    child.remove();
    const index = next ? this.children.indexOf(next) : this.children.length;
    if (index < 0) throw new Error("insertBefore reference is not a child");
    this.children.splice(index, 0, child);
    child.parent = this;
  }

  remove() {
    if (this.parent) this.parent.children = this.parent.children.filter((child) => child !== this);
    this.parent = null;
  }

  get firstChild(): Element | null { return this.children[0] ?? null; }
  get nextSibling(): Element | null { return this.parent?.children[this.parent.children.indexOf(this) + 1] ?? null; }
  setAttribute(name: string, value: string) { this.attributes.set(name, value); }
  focus() { if (this.ownerDocument) this.ownerDocument.activeElement = this; }

  addEventListener(name: string, listener: (event: any) => void) {
    if (!this.listeners.has(name)) this.listeners.set(name, new Set());
    this.listeners.get(name)!.add(listener);
  }

  removeEventListener(name: string, listener: (event: any) => void) {
    this.listeners.get(name)?.delete(listener);
  }

  emit(name: string, props: Record<string, unknown> = {}) {
    let stopped = false;
    const event = {
      target: this, preventDefault() {}, stopPropagation() { stopped = true; }, ...props,
    };
    for (let node: Element | null = this; node; node = node.parent) {
      for (const listener of [...(node.listeners.get(name) ?? [])]) listener(event);
      if (stopped) break;
    }
  }

  querySelectorAll(selector: string): Element[] {
    const matches = (node: Element) => selector.startsWith(".")
      ? node.classList.contains(selector.slice(1)) : node.tag === selector;
    return this.children.flatMap((child) => [
      ...(matches(child) ? [child] : []), ...child.querySelectorAll(selector),
    ]);
  }

  querySelector(selector: string) { return this.querySelectorAll(selector)[0] ?? null; }
}

export class TestDocument extends Element {
  body = new Element("body", this);
  activeElement: Element | null = null;
  constructor() { super("document"); this.append(this.body); }
  createElement(tag: string) { return new Element(tag, this); }
}
