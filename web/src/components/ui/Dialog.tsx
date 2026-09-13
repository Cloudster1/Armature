import { useCallback, useEffect, useId, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { DRAWER_WIDTH } from "@/config";
import { Button, IconButton } from "./Button";
import { cx } from "./cx";
import { useEscape, useFocusReturn, useScrollLock } from "./overlay";
import { Icon } from "../icons";

type DialogProps = {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  size?: "sm" | "md" | "lg";
  /** Forwarded to the dialog element, for the attributes tests read. */
  attrs?: Record<string, string>;
};

const dialogWidth = { sm: "max-w-sm", md: "max-w-lg", lg: "max-w-3xl" };

// Modal: the page behind is inert, focus is trapped, Escape closes, and the
// dialog is portaled last in the DOM so it is on top and easy to find.
export function Dialog({ open, onClose, title, description, children, footer, size = "md", attrs }: DialogProps) {
  const ref = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const descriptionId = useId();
  const close = useCallback(() => onClose(), [onClose]);
  useEscape(open, close);
  useScrollLock(open);
  useFocusReturn(open, ref, true);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-ink/40 p-4 pt-[12vh]" onPointerDown={(e) => e.target === e.currentTarget && close()}>
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        {...attrs}
        className={cx("w-full rounded-overlay border border-border bg-surface-overlay shadow-2", dialogWidth[size])}
      >
        <div className="flex items-start gap-3 px-5 pt-4">
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className="text-base font-semibold text-ink">
              {title}
            </h2>
            {description && (
              <p id={descriptionId} className="mt-1 text-sm text-ink-muted">
                {description}
              </p>
            )}
          </div>
          <IconButton icon={<Icon.X />} label="Close" size="sm" onClick={close} data-focus-last="" />
        </div>
        {children && <div className="px-5 py-4">{children}</div>}
        {footer && <div className="flex justify-end gap-2 border-t border-border px-5 py-3">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

// The one dialog that stands between a click and a deletion. The button says
// the verb and the noun, so the reader knows what goes before it goes.
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  noun,
  verb = "Delete",
  body,
  loading,
  error,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  noun: string;
  verb?: string;
  body: ReactNode;
  loading?: boolean;
  error?: ReactNode;
}) {
  return (
    <Dialog open={open} onClose={onClose} title={`${verb} ${noun}?`} size="sm" attrs={{ "data-confirm": noun }}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="danger" onClick={onConfirm} loading={loading} data-action="confirm">
            {verb} {noun}
          </Button>
        </>
      }
    >
      <div className="space-y-2 text-sm text-ink">
        {body}
        {error && <p className="text-danger">{error}</p>}
      </div>
    </Dialog>
  );
}

type PanelProps = {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  children: ReactNode;
  width?: number;
  /** Controls beside the close button: previous, next, open the page. */
  actions?: ReactNode;
  attrs?: Record<string, string>;
};

/** The header and scrolling body the drawer and the docked panel share. */
function PanelHeader({ titleId, title, actions, onClose }: { titleId: string; title: ReactNode; actions?: ReactNode; onClose: () => void }) {
  return (
    <div className="flex items-center gap-2 border-b border-border px-5 py-3">
      <h2 id={titleId} className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">
        {title}
      </h2>
      {actions}
      <IconButton icon={<Icon.X />} label="Close" size="sm" onClick={onClose} data-focus-last="" />
    </div>
  );
}

/** A panel from the right that keeps the page behind it in view but not in reach. */
export function Drawer({ open, onClose, title, children, width = DRAWER_WIDTH, actions, attrs }: PanelProps) {
  const ref = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const close = useCallback(() => onClose(), [onClose]);
  useEscape(open, close);
  useFocusReturn(open, ref, true);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-50 flex justify-end bg-ink/30" onPointerDown={(e) => e.target === e.currentTarget && close()}>
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        {...attrs}
        style={{ width: `min(${width}px, 100vw)` }}
        className="flex h-full flex-col border-l border-border bg-surface shadow-2"
      >
        <PanelHeader titleId={titleId} title={title} actions={actions} onClose={close} />
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
      </div>
    </div>,
    document.body,
  );
}

const EDITABLE = 'input, textarea, select, [contenteditable=""], [contenteditable="true"]';

/** Whether a key pressed on this element was meant for typing, not for the page. */
export function isEditing(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(EDITABLE) !== null;
}

const INTERACTIVE = "a, button, input, select, label";

/** Whether a click landed on something of its own, so a row around it should not also act. */
export function isInteractiveTarget(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(INTERACTIVE) !== null;
}

// A column beside the page rather than a layer over it: no scrim, no focus
// trap, and Escape closes it only when nothing is being typed and no dialog
// is open above it, so the list stays in reach while the panel is up.
export function DockedPanel({ open, onClose, title, children, width = DRAWER_WIDTH, actions, attrs }: PanelProps) {
  const ref = useRef<HTMLElement>(null);
  const titleId = useId();
  const close = useCallback(() => onClose(), [onClose]);
  useEffect(() => {
    if (!open) return;
    function onKey(event: KeyboardEvent) {
      if (event.key !== "Escape" || event.defaultPrevented || isEditing(event.target)) return;
      if (document.querySelector('[role="dialog"][aria-modal="true"]')) return;
      close();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, close]);
  useFocusReturn(open, ref, false);
  if (!open) return null;
  return (
    <aside
      ref={ref}
      role="complementary"
      aria-labelledby={titleId}
      {...attrs}
      style={{ width }}
      className="flex h-full shrink-0 flex-col border-l border-border bg-surface"
    >
      <PanelHeader titleId={titleId} title={title} actions={actions} onClose={close} />
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
    </aside>
  );
}
