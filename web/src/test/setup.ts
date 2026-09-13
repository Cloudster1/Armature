import "@testing-library/jest-dom/vitest";

/**
 * Node 26 ships an experimental built-in `localStorage` that is inert unless the
 * runtime is started with --localstorage-file, and it takes precedence over the
 * one jsdom provides. The result is that `localStorage` is undefined inside
 * tests even though the browser the code actually runs in has it.
 *
 * Rather than pin the whole toolchain to an older Node, install a minimal
 * in-memory implementation of the Storage interface. Tests that need to
 * simulate a browser refusing access still can, by spying on Storage.prototype.
 */
class MemoryStorage implements Storage {
  #entries = new Map<string, string>();

  get length(): number {
    return this.#entries.size;
  }

  clear(): void {
    this.#entries.clear();
  }

  getItem(key: string): string | null {
    return this.#entries.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.#entries.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.#entries.delete(key);
  }

  setItem(key: string, value: string): void {
    this.#entries.set(key, String(value));
  }
}

if (typeof globalThis.Storage === "undefined") {
  Object.defineProperty(globalThis, "Storage", { value: MemoryStorage, writable: true });
}

if (!globalThis.localStorage) {
  const storage = new MemoryStorage();
  Object.defineProperty(globalThis, "localStorage", { value: storage, writable: true });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", { value: storage, writable: true });
  }
}

/**
 * React Flow measures its nodes and handles before it draws anything, and
 * jsdom measures nothing. These are the stubs its own testing guide asks for,
 * each only where jsdom has left a gap; a test that installs its own
 * ResizeObserver keeps it. Dragging still does not run under jsdom.
 */
if (typeof globalThis.ResizeObserver === "undefined") {
  class StubResizeObserver implements ResizeObserver {
    #callback: ResizeObserverCallback;
    #stopped = false;
    constructor(callback: ResizeObserverCallback) {
      this.#callback = callback;
    }
    observe(target: Element): void {
      queueMicrotask(() => {
        if (this.#stopped) return;
        const contentRect = target.getBoundingClientRect();
        this.#callback([{ target, contentRect } as ResizeObserverEntry], this);
      });
    }
    unobserve(): void {}
    disconnect(): void {
      this.#stopped = true;
    }
  }
  Object.defineProperty(globalThis, "ResizeObserver", { value: StubResizeObserver, writable: true });
}
if (typeof globalThis.DOMMatrixReadOnly === "undefined") {
  class StubDOMMatrixReadOnly {
    m22: number;
    constructor(transform?: string) {
      const scale = transform?.match(/scale\(([1-9.])\)/)?.[1];
      this.m22 = scale !== undefined ? Number(scale) : 1;
    }
  }
  Object.defineProperty(globalThis, "DOMMatrixReadOnly", { value: StubDOMMatrixReadOnly, writable: true });
}
if (typeof window !== "undefined") {
  const sized = (dimension: "width" | "height") =>
    function (this: HTMLElement) {
      return parseFloat(this.style[dimension]) || 1;
    };
  Object.defineProperties(window.HTMLElement.prototype, {
    offsetHeight: { get: sized("height"), configurable: true },
    offsetWidth: { get: sized("width"), configurable: true },
  });
  if (!(window.SVGElement.prototype as { getBBox?: unknown }).getBBox) {
    (window.SVGElement.prototype as unknown as { getBBox: () => DOMRect }).getBBox = () =>
      ({ x: 0, y: 0, width: 0, height: 0 }) as DOMRect;
  }
}

// ProseMirror measures the selection and the pointer; jsdom draws nothing, so
// every measurement answers zero and every point finds nothing.
if (typeof window !== "undefined") {
  const zeroRect = () => ({ x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, toJSON: () => ({}) }) as DOMRect;
  const rangeProto = window.Range.prototype as unknown as { getClientRects?: () => DOMRectList; getBoundingClientRect?: () => DOMRect };
  if (!rangeProto.getClientRects) {
    rangeProto.getClientRects = () => ({ length: 0, item: () => null, [Symbol.iterator]: [][Symbol.iterator] }) as unknown as DOMRectList;
  }
  if (!rangeProto.getBoundingClientRect) rangeProto.getBoundingClientRect = zeroRect;
  if (!document.elementFromPoint) {
    (document as unknown as { elementFromPoint: () => Element | null }).elementFromPoint = () => null;
  }
  for (const name of ["ClipboardEvent", "DragEvent"] as const) {
    if (typeof (globalThis as Record<string, unknown>)[name] === "undefined") {
      Object.defineProperty(globalThis, name, {
        value: class extends Event {
          clipboardData = null;
          dataTransfer = null;
        },
        writable: true,
      });
    }
  }
}
