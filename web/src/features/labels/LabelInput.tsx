import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import type { LabelRef } from "@/api/issues";
import { useLabelSearch } from "@/api/labels";
import { Input, cx } from "@/components/ui";
import type { ControlSize } from "@/components/ui/Button";
import { LABEL_SEARCH_DELAY_MS } from "@/config";
import { useDebounced } from "@/lib/debounce";
import { LABEL_CLASSES, LabelChip } from "./LabelChip";

/** One row under the box: a label the server has, or the typed word to create. */
interface Row {
  name: string;
  label?: LabelRef;
}

const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

/**
 * Labels are everybody's, so the box draws none until something is typed and
 * then offers only the few that hold it, and the typed word.
 */
export function LabelInput({
  value,
  onChange,
  mode = "many",
  create = true,
  known = [],
  id,
  "aria-label": ariaLabel,
  placeholder = "Type a label",
  controlSize = "md",
  align = "start",
  commitOnBlur = create,
  onTyping,
  className,
}: {
  value: string[];
  /** The names now chosen, and the labels among them the server already has. */
  onChange: (names: string[], labels: LabelRef[]) => void;
  /** One holds a single label, the newest replacing it. */
  mode?: "many" | "one";
  /** Offer the typed word as a new label; a filter picks only labels that exist. */
  create?: boolean;
  /** Labels the caller already holds, so their chips keep their colour. */
  known?: Array<Pick<LabelRef, "id" | "name" | "color">>;
  id?: string;
  "aria-label"?: string;
  placeholder?: string;
  controlSize?: ControlSize;
  align?: "start" | "end";
  commitOnBlur?: boolean;
  /** The word typed but not yet added, for a caller whose button must count it. */
  onTyping?: (word: string) => void;
  className?: string;
}) {
  const [draft, setDraft] = useState("");
  const [focused, setFocused] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const [active, setActive] = useState(-1);
  // Every label the server has shown, so a chosen one keeps its colour and id.
  const seen = useRef(new Map<string, LabelRef>());
  for (const label of known) seen.current.set(label.name.toLowerCase(), label);
  const listId = `${useId()}-labels`;

  const typed = draft.trim().replace(/,+$/, "");
  useEffect(() => onTyping?.(typed), [typed, onTyping]);
  const settled = useDebounced(typed, LABEL_SEARCH_DELAY_MS);
  const { data } = useLabelSearch(settled);
  const found = typed && data ? data.labels : [];
  for (const label of found) seen.current.set(label.name.toLowerCase(), label);

  const rows: Row[] = found.filter((label) => !value.some((name) => same(name, label.name))).map((label) => ({ name: label.name, label }));
  const taken = found.some((label) => same(label.name, typed)) || value.some((name) => same(name, typed));
  if (create && typed && !taken) rows.push({ name: typed });
  const open = focused && !dismissed && typed !== "" && rows.length > 0;

  const chosen = (names: string[]) => names.map((name) => seen.current.get(name.toLowerCase())).filter((label): label is LabelRef => label !== undefined);

  function add(row: Row | undefined) {
    if (!row) return;
    setDraft("");
    setActive(-1);
    if (value.some((name) => same(name, row.name))) return;
    const names = mode === "one" ? [row.name] : [...value, row.name];
    onChange(names, chosen(names));
  }

  /** The typed word as the label the server has by that name, or as a new one. */
  function addTyped() {
    if (!typed) return;
    const existing = seen.current.get(typed.toLowerCase());
    if (existing) add({ name: existing.name, label: existing });
    else if (create) add({ name: typed });
  }

  function remove(name: string) {
    const names = value.filter((each) => each !== name);
    onChange(names, chosen(names));
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    switch (event.key) {
      case "ArrowDown":
        if (!open) return;
        event.preventDefault();
        setActive((i) => (i + 1) % rows.length);
        break;
      case "ArrowUp":
        if (!open) return;
        event.preventDefault();
        setActive((i) => (i <= 0 ? rows.length - 1 : i - 1));
        break;
      case "Enter":
      case ",":
        // An empty box lets Enter submit the form around it.
        if (event.key === "Enter" && !typed) return;
        event.preventDefault();
        if (open && active >= 0) add(rows[active]);
        else addTyped();
        break;
      case "Escape":
        if (!open) return;
        event.preventDefault();
        event.stopPropagation();
        setDismissed(true);
        setActive(-1);
        break;
      case "Backspace": {
        const last = value.at(-1);
        if (draft === "" && last !== undefined) remove(last);
        break;
      }
    }
  }

  return (
    <div className={cx("relative flex flex-col gap-1", align === "end" ? "items-end" : "items-stretch", className)}>
      {value.length > 0 && (
        <span className={cx("flex flex-wrap gap-1", align === "end" && "justify-end")}>
          {value.map((name) => (
            <LabelChip key={name} label={seen.current.get(name.toLowerCase()) ?? { name, color: "gray" }} onRemove={() => remove(name)} />
          ))}
        </span>
      )}
      <Input
        id={id}
        type="text"
        role="combobox"
        aria-label={ariaLabel}
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={open && active >= 0 ? `${listId}-${active}` : undefined}
        autoComplete="off"
        spellCheck={false}
        controlSize={controlSize}
        value={draft}
        placeholder={placeholder}
        onChange={(event) => {
          setDraft(event.target.value);
          setDismissed(false);
          setActive(-1);
        }}
        onKeyDown={onKeyDown}
        onFocus={() => setFocused(true)}
        onBlur={() => {
          setFocused(false);
          setActive(-1);
          if (commitOnBlur) addTyped();
        }}
        className={cx(align === "end" && "text-right", controlSize === "sm" && "text-xs")}
      />
      {open && (
        <div
          id={listId}
          role="listbox"
          aria-label="Labels"
          data-label-suggestions
          className="absolute left-0 right-0 top-full z-30 mt-1 max-h-60 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 text-left shadow-2"
        >
          {rows.map((row, i) => (
            <div
              key={row.label?.id ?? `new:${row.name}`}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === active}
              data-label-option={row.name}
              onMouseDown={(event) => {
                // Kept from the box, so its blur does not add the typed word first.
                event.preventDefault();
                add(row);
              }}
              onMouseEnter={() => setActive(i)}
              className={cx("flex cursor-pointer items-center gap-2 rounded-control px-2 py-1.5 text-sm", i === active ? "bg-surface-raised text-ink" : "text-ink-muted")}
            >
              {row.label ? (
                <>
                  <span aria-hidden="true" className={cx("size-2.5 shrink-0 rounded-full", LABEL_CLASSES[row.label.color] ?? LABEL_CLASSES.gray)} />
                  <span className="truncate text-ink">{row.name}</span>
                </>
              ) : (
                <span className="truncate">
                  Create <span className="font-medium text-ink">{row.name}</span>
                </span>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
