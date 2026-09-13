import { useEffect, useRef, type RefObject } from "react";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function focusables(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
}

/** Calls onClose on Escape while open. */
export function useEscape(open: boolean, onClose: () => void) {
  useEffect(() => {
    if (!open) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.stopPropagation();
        onClose();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);
}

/** Calls onClose on a pointer press outside every ref given. */
export function useOutsidePress(open: boolean, refs: Array<RefObject<HTMLElement | null>>, onClose: () => void) {
  useEffect(() => {
    if (!open) return;
    function onPress(event: PointerEvent) {
      const target = event.target as Node | null;
      if (refs.some((ref) => ref.current?.contains(target))) return;
      onClose();
    }
    document.addEventListener("pointerdown", onPress);
    return () => document.removeEventListener("pointerdown", onPress);
  }, [open, refs, onClose]);
}

// Focus goes into the overlay when it opens and back to where it was when it
// closes, so a keyboard user is never left on an element that has gone.
export function useFocusReturn(open: boolean, into: RefObject<HTMLElement | null>, trap: boolean) {
  const previous = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (!open) return;
    previous.current = document.activeElement as HTMLElement | null;
    const root = into.current;
    // The close button is first in the DOM but last in intent: focus lands on
    // the first thing the reader came to do.
    const candidates = root ? focusables(root) : [];
    const first = candidates.find((el) => !el.hasAttribute("data-focus-last")) ?? candidates[0] ?? root;
    first?.focus({ preventScroll: true });
    function onKey(event: KeyboardEvent) {
      if (!trap || event.key !== "Tab" || !root) return;
      const items = focusables(root);
      if (items.length === 0) {
        event.preventDefault();
        return;
      }
      const firstItem = items[0]!;
      const lastItem = items[items.length - 1]!;
      if (event.shiftKey && document.activeElement === firstItem) {
        event.preventDefault();
        lastItem.focus();
      } else if (!event.shiftKey && document.activeElement === lastItem) {
        event.preventDefault();
        firstItem.focus();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      previous.current?.focus({ preventScroll: true });
    };
  }, [open, into, trap]);
}

/** Stops the page behind a modal from scrolling while it is open. */
export function useScrollLock(open: boolean) {
  useEffect(() => {
    if (!open) return;
    const before = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = before;
    };
  }, [open]);
}

let counter = 0;
/** A stable id for aria wiring; React's useId is fine too, this one reads better in tests. */
export function nextId(prefix: string): string {
  counter += 1;
  return `${prefix}-${counter}`;
}
