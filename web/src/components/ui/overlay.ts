import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
import { MENU_GAP_PX } from "@/config";

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

export type AnchoredSide = "top" | "bottom" | "right";

/**
 * Where a panel goes beside the element it belongs to, as fixed viewport
 * coordinates, so it can be drawn at the end of the document where no
 * scrolling box can cut it off. Below the anchor by default, above when the
 * bottom is near; to its right when asked. Recomputed on scroll and resize
 * while open. Hidden until the first measurement so nothing flashes.
 */
export function useAnchored(
  open: boolean,
  anchorRef: RefObject<HTMLElement | null>,
  panelRef: RefObject<HTMLElement | null>,
  { align = "start", side = "bottom" }: { align?: "start" | "end"; side?: AnchoredSide } = {},
): CSSProperties {
  const [style, setStyle] = useState<CSSProperties>({ visibility: "hidden" });
  const place = useCallback(() => {
    const anchor = anchorRef.current?.getBoundingClientRect();
    const panel = panelRef.current;
    if (!anchor || !panel) return;
    const gap = MENU_GAP_PX;
    const width = panel.offsetWidth;
    const height = panel.offsetHeight;
    const next: CSSProperties = {};
    if (side === "right") {
      next.left = Math.min(anchor.right + gap, window.innerWidth - width);
      next.top = Math.max(0, Math.min(anchor.top + anchor.height / 2 - height / 2, window.innerHeight - height));
    } else {
      const below = anchor.bottom + gap;
      const above = anchor.top - gap - height;
      const goesBelow = side === "bottom" ? below + height <= window.innerHeight || above < 0 : above < 0;
      next.top = goesBelow ? below : Math.max(0, above);
      if (align === "end") next.right = Math.max(0, window.innerWidth - anchor.right);
      else next.left = Math.max(0, Math.min(anchor.left, window.innerWidth - width));
    }
    setStyle(next);
  }, [anchorRef, panelRef, align, side]);
  useLayoutEffect(() => {
    if (!open) {
      setStyle({ visibility: "hidden" });
      return;
    }
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open, place]);
  return style;
}
