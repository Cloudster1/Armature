import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { cx } from "./cx";
import { focusables, useEscape, useOutsidePress } from "./overlay";

export interface MenuItem {
  label: ReactNode;
  onSelect: () => void;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  /** Forwarded to the item, for the attributes tests read. */
  attrs?: Record<string, string>;
}

// A list of actions behind one button: arrows move, Home and End jump, typing
// a letter finds, Escape returns focus to the trigger.
export function Menu({
  trigger,
  items,
  label,
  align = "start",
  className,
}: {
  trigger: (props: { open: boolean; toggle: () => void; "aria-haspopup": "menu"; "aria-expanded": boolean }) => ReactNode;
  items: MenuItem[];
  label: string;
  align?: "start" | "end";
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  // A press elsewhere moved the reader on; only a keyboard close or a pick
  // brings focus back to the trigger.
  const leaveFocus = useRef(false);
  const close = useCallback(() => setOpen(false), []);
  const closeFromOutside = useCallback(() => {
    leaveFocus.current = true;
    setOpen(false);
  }, []);
  useEscape(open, close);
  useOutsidePress(open, [wrapRef], closeFromOutside);

  const wasOpen = useRef(false);
  useEffect(() => {
    if (open) {
      focusables(listRef.current!)[0]?.focus();
    } else if (wasOpen.current && !leaveFocus.current) {
      focusables(wrapRef.current!)[0]?.focus();
    }
    wasOpen.current = open;
    leaveFocus.current = false;
  }, [open]);

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const list = listRef.current;
    if (!list) return;
    const options = focusables(list);
    const index = options.indexOf(document.activeElement as HTMLElement);
    const go = (i: number) => options[(i + options.length) % options.length]?.focus();
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        go(index + 1);
        break;
      case "ArrowUp":
        event.preventDefault();
        go(index - 1);
        break;
      case "Home":
        event.preventDefault();
        go(0);
        break;
      case "End":
        event.preventDefault();
        go(options.length - 1);
        break;
      default:
        if (event.key.length === 1 && /\S/.test(event.key)) {
          const found = options.find((el, i) => i > index && el.textContent?.trim().toLowerCase().startsWith(event.key.toLowerCase())) ??
            options.find((el) => el.textContent?.trim().toLowerCase().startsWith(event.key.toLowerCase()));
          found?.focus();
        }
    }
  }

  return (
    <div ref={wrapRef} className={cx("relative inline-flex", className)}>
      {trigger({ open, toggle: () => setOpen((o) => !o), "aria-haspopup": "menu", "aria-expanded": open })}
      {open && (
        <div
          ref={listRef}
          role="menu"
          aria-label={label}
          onKeyDown={onKeyDown}
          className={cx("absolute top-full z-30 mt-1 min-w-44 rounded-overlay border border-border bg-surface-overlay p-1 shadow-2", align === "end" ? "right-0" : "left-0")}
        >
          {items.map((item, i) => (
            <button
              key={i}
              type="button"
              role="menuitem"
              disabled={item.disabled}
              tabIndex={-1}
              {...item.attrs}
              onClick={() => {
                setOpen(false);
                item.onSelect();
              }}
              className={cx(
                "flex w-full items-center gap-2 rounded-control px-2 py-1.5 text-left text-sm",
                item.danger ? "text-danger hover:bg-danger-subtle" : "text-ink hover:bg-surface-raised",
                "focus:bg-surface-raised focus:outline-none disabled:opacity-50",
              )}
            >
              {item.icon && <span className="text-ink-muted [&_svg]:size-4">{item.icon}</span>}
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
