import { useCallback, useRef, type ReactNode } from "react";
import { cx } from "./cx";
import { useEscape, useFocusReturn, useOutsidePress } from "./overlay";

// A panel anchored to its trigger and not modal: the page stays live, Escape
// or a press outside closes it, and focus goes in on open and back on close.
export function Popover({
  open,
  onClose,
  trigger,
  children,
  align = "start",
  label,
  className,
}: {
  open: boolean;
  onClose: () => void;
  trigger: ReactNode;
  children: ReactNode;
  align?: "start" | "end";
  label: string;
  className?: string;
}) {
  const triggerRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const close = useCallback(() => onClose(), [onClose]);
  useEscape(open, close);
  useOutsidePress(open, [triggerRef, panelRef], close);
  useFocusReturn(open, panelRef, false);
  return (
    <div className="relative inline-flex">
      <div ref={triggerRef} className="inline-flex">
        {trigger}
      </div>
      {open && (
        <div
          ref={panelRef}
          role="dialog"
          aria-label={label}
          data-popover
          className={cx(
            "absolute top-full z-30 mt-1 min-w-56 rounded-overlay border border-border bg-surface-overlay p-3 shadow-2",
            align === "end" ? "right-0" : "left-0",
            className,
          )}
        >
          {children}
        </div>
      )}
    </div>
  );
}
