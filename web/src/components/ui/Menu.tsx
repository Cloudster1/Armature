import { Fragment, useCallback, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { cx } from "./cx";
import { focusables, isEditing, useAnchored, useEscape, useOutsidePress } from "./overlay";

export interface MenuItem {
  label: ReactNode;
  /** What picking it does; an item with an href is a link and needs none. */
  onSelect?: () => void;
  /** Makes the item a plain link, so the browser can open it in a new tab too. */
  href?: string;
  /** Starts a group under this heading, which runs to the next item that starts one. */
  section?: string;
  /** Sets the item apart from those above it by a line, without a heading. */
  separated?: boolean;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  /** Forwarded to the item, for the attributes tests read. */
  attrs?: Record<string, string>;
}

// A list of actions behind one button: arrows move, Home and End jump, typing
// a letter finds, Escape returns focus to the trigger. The list is drawn at
// the end of the document, fixed beside the trigger, so a table or a panel
// that scrolls cannot cut it off; it opens upward when the bottom is near.
export function Menu({
  trigger,
  items,
  label,
  header,
  align = "start",
  className,
}: {
  trigger: (props: { open: boolean; toggle: () => void; "aria-haspopup": "menu"; "aria-expanded": boolean }) => ReactNode;
  items: MenuItem[];
  label: string;
  /** Drawn above the items and outside them, such as a box that narrows them: inside an item's button, Space would pick it. */
  header?: ReactNode;
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
  const outside = useRef([wrapRef, listRef]);
  useOutsidePress(open, outside.current, closeFromOutside);

  const place = useAnchored(open, wrapRef, listRef, { align });

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
    // In a box the keys are for typing; only the arrows leave it for the items.
    if (isEditing(event.target) && event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
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
      {open &&
        createPortal(
        <div
          ref={listRef}
          role="menu"
          aria-label={label}
          onKeyDown={onKeyDown}
          style={place}
          className="fixed z-30 min-w-44 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
        >
          {header}
          {items.map((item, i) => {
            const className = cx(
              "flex w-full items-center gap-2 rounded-control px-2 py-1.5 text-left text-sm",
              item.danger ? "text-danger hover:bg-danger-subtle" : "text-ink hover:bg-surface-raised",
              "focus:bg-surface-raised focus:outline-none disabled:opacity-50",
            );
            const content = (
              <>
                {item.icon && <span className="text-ink-muted [&_svg]:size-4">{item.icon}</span>}
                {item.label}
              </>
            );
            return (
              <Fragment key={i}>
                {item.separated && !item.section && i > 0 && <div role="separator" className="my-1 border-t border-border" />}
                {item.section && (
                  <div role="presentation" className={cx("px-2 pt-2 pb-1 text-2xs font-semibold text-ink-subtle", i > 0 && "mt-1 border-t border-border")} data-menu-section={item.section}>
                    {item.section}
                  </div>
                )}
                {item.href ? (
                  <a role="menuitem" href={item.href} tabIndex={-1} {...item.attrs} onClick={() => setOpen(false)} className={className}>
                    {content}
                  </a>
                ) : (
                  <button
                    type="button"
                    role="menuitem"
                    disabled={item.disabled}
                    tabIndex={-1}
                    {...item.attrs}
                    onClick={() => {
                      setOpen(false);
                      item.onSelect?.();
                    }}
                    className={className}
                  >
                    {content}
                  </button>
                )}
              </Fragment>
            );
          })}
        </div>,
        document.body,
        )}
    </div>
  );
}
