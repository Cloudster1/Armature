import { cx } from "@/components/ui";
import { MENU_GAP_PX, POPOVER_EDGE_PX, POPOVER_MAX_HEIGHT_PX } from "@/config";
import type { Mentionable } from "./schema";

/**
 * The people an at sign could mean, drawn under the caret in the rich
 * editor. Same markup as the plain textarea's list, so a test reads either.
 */
export function MentionList({
  items,
  active,
  rect,
  onPick,
  onHover,
}: {
  items: Mentionable[];
  active: number;
  rect: DOMRect | null;
  onPick: (person: Mentionable) => void;
  onHover: (i: number) => void;
}) {
  if (items.length === 0 || !rect) return null;
  const top = rect.bottom + MENU_GAP_PX;
  return (
    <div
      role="listbox"
      aria-label="People to mention"
      data-mention-list
      className="fixed z-40 min-w-56 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2"
      style={{ left: rect.left, top, maxHeight: Math.min(POPOVER_MAX_HEIGHT_PX, window.innerHeight - top - POPOVER_EDGE_PX) }}
    >
      {items.map((person, i) => (
        <div
          key={person.id}
          role="option"
          aria-selected={i === active}
          data-mention-option={person.name}
          onMouseDown={(e) => {
            e.preventDefault();
            onPick(person);
          }}
          onMouseEnter={() => onHover(i)}
          className={cx(
            "flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm",
            i === active ? "bg-surface-raised text-ink" : "text-ink-muted",
          )}
        >
          <span className="font-medium text-ink">{person.name}</span>
          {person.email && <span className="truncate text-xs text-ink-subtle">{person.email}</span>}
        </div>
      ))}
    </div>
  );
}
