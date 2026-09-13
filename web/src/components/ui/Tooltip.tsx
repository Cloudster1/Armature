import { useId, useRef, useState, type ReactNode } from "react";
import { TOOLTIP_DELAY_MS } from "@/config";
import { cx } from "./cx";

// A tooltip explains, it never names: the thing it sits on has its own label.
// It waits before showing so a pointer passing over shows nothing.
export function Tooltip({ text, children, side = "bottom" }: { text: string; children: ReactNode; side?: "top" | "bottom" | "right" }) {
  const [shown, setShown] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const id = useId();
  function show() {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setShown(true), TOOLTIP_DELAY_MS);
  }
  function hide() {
    window.clearTimeout(timer.current);
    setShown(false);
  }
  const place = side === "top" ? "bottom-full mb-1 left-1/2 -translate-x-1/2" : side === "right" ? "left-full ml-1 top-1/2 -translate-y-1/2" : "top-full mt-1 left-1/2 -translate-x-1/2";
  return (
    <span className="relative inline-flex" onPointerEnter={show} onPointerLeave={hide} onFocus={show} onBlur={hide} aria-describedby={shown ? id : undefined}>
      {children}
      {shown && (
        <span role="tooltip" id={id} className={cx("absolute z-40 rounded-control bg-primary px-2 py-1 text-xs whitespace-nowrap text-on-primary shadow-1", place)}>
          {text}
        </span>
      )}
    </span>
  );
}
