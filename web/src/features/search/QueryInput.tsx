import { useDeferredValue, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { useSuggestions, type Completion } from "@/api/issues";
import { useSavedFilters } from "@/api/filters";
import { Button, IconButton, Input, Popover, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SUGGEST_MIN_CHARS, SUGGEST_RESULTS } from "@/config";
import { QUERY_CUSTOM_FIELD_EXAMPLE, QUERY_EXAMPLES, QUERY_FIELDS, QUERY_FUNCTIONS, QUERY_OPERATORS, caretLine } from "./help";
import { useRecentQueries } from "./recent";

/** One row under the bar: a word to put in, an issue to open, or a query to run again. */
interface Suggestion {
  kind: "completion" | "issue" | "recent" | "filter";
  text: string;
  detail?: string;
  /** For a completion: the characters it replaces. */
  from?: number;
  to?: number;
  quoted?: boolean;
  /** For an issue: where it opens. */
  issueKey?: string;
}

/** Puts a chosen word into the text where the caret's word was, and says where the caret lands. */
export function applyCompletion(text: string, completion: Pick<Completion, "from" | "to" | "quoted">, word: string): { text: string; caret: number } {
  const chars = Array.from(text);
  const closing = completion.quoted && !word.startsWith('"') ? '"' : "";
  const opening = completion.quoted && !word.startsWith('"') ? '"' : "";
  const inserted = `${opening}${word}${closing} `;
  const next = [...chars.slice(0, completion.from), ...Array.from(inserted), ...chars.slice(completion.to)].join("");
  return { text: next, caret: completion.from + Array.from(inserted).length };
}

/**
 * Where a query is typed. Enter applies it; the text is kept as typed, and an
 * error is shown under it with a caret at the character the server named.
 * As it is typed, a list under it offers the words the query takes at the
 * caret and the issues the words so far find; an empty bar offers what was
 * searched before.
 */
export function QueryInput({
  value,
  onSubmit,
  error,
  compact = false,
  autoFocus = false,
  label = "Query",
  project,
}: {
  value: string;
  onSubmit: (query: string) => void;
  error?: unknown;
  /** No help and no button: Enter is the whole interface. */
  compact?: boolean;
  autoFocus?: boolean;
  label?: string;
  /** The project the page is about, which scopes the names and the issues offered. */
  project?: string;
}) {
  const [draft, setDraft] = useState(value);
  const [help, setHelp] = useState(false);
  const [caret, setCaret] = useState(value.length);
  const [focused, setFocused] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  // The list opens on typing or Arrow Down, never on focus alone, so a page
  // that focuses the bar on arrival is not covered by it.
  const [asked, setAsked] = useState(false);
  const [active, setActive] = useState(-1);
  const ref = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  useEffect(() => setDraft(value), [value]);

  const failure = describe(error);

  // The request follows the typing at the pace React can spare, and the
  // last answer stays until the next arrives, so nothing flickers.
  const deferred = useDeferredValue({ q: draft, at: Math.min(caret, draft.length) });
  const showing = focused && asked && !dismissed;
  const asking = showing && draft.length >= SUGGEST_MIN_CHARS;
  const { data: suggested } = useSuggestions({ q: deferred.q, at: deferred.at, project }, asking);
  const [recent] = useRecentQueries();
  const { data: saved } = useSavedFilters();

  const rows: Suggestion[] = [];
  if (showing) {
    if (draft.length < SUGGEST_MIN_CHARS && !compact) {
      for (const q of recent.slice(0, SUGGEST_RESULTS)) rows.push({ kind: "recent", text: q });
      for (const f of (saved?.filters ?? []).slice(0, SUGGEST_RESULTS)) rows.push({ kind: "filter", text: f.query, detail: f.name });
    } else if (suggested) {
      const c = suggested.completion;
      for (const w of suggested.words) rows.push({ kind: "completion", text: w.text, detail: w.detail, from: c.from, to: c.to, quoted: c.quoted });
      for (const issue of suggested.issues) rows.push({ kind: "issue", text: issue.summary, detail: issue.key, issueKey: issue.key });
    }
  }
  const open = rows.length > 0;

  function track(input: HTMLInputElement) {
    setCaret(input.selectionStart ?? input.value.length);
  }

  function choose(row: Suggestion) {
    switch (row.kind) {
      case "completion": {
        const next = applyCompletion(draft, { from: row.from ?? draft.length, to: row.to ?? draft.length, quoted: row.quoted ?? false }, row.text);
        setDraft(next.text);
        setCaret(next.caret);
        setActive(-1);
        requestAnimationFrame(() => {
          ref.current?.focus();
          ref.current?.setSelectionRange(next.caret, next.caret);
        });
        break;
      }
      case "issue":
        setDismissed(true);
        if (row.issueKey) navigate({ to: "/issues/$issueKey", params: { issueKey: row.issueKey } });
        break;
      case "recent":
      case "filter":
        setDraft(row.text);
        setDismissed(true);
        onSubmit(row.text);
        break;
    }
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (!open) {
      if (event.key === "ArrowDown") {
        setAsked(true);
        setDismissed(false);
      }
      return;
    }
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        setActive((i) => (i + 1) % rows.length);
        break;
      case "ArrowUp":
        event.preventDefault();
        setActive((i) => (i <= 0 ? rows.length - 1 : i - 1));
        break;
      case "Enter":
        // Nothing picked: Enter runs the query, as it always has.
        if (active >= 0) {
          event.preventDefault();
          choose(rows[active]!);
        }
        break;
      case "Tab": {
        const first = rows.find((row) => row.kind === "completion");
        if (first && !event.shiftKey) {
          event.preventDefault();
          choose(active >= 0 && rows[active]?.kind === "completion" ? rows[active]! : first);
        }
        break;
      }
      case "Escape":
        event.preventDefault();
        event.stopPropagation();
        setDismissed(true);
        setActive(-1);
        break;
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    setDismissed(true);
    onSubmit(draft.trim());
  }

  const listId = `${label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}-suggestions`;

  return (
    <form onSubmit={submit} className={cx("min-w-0", compact ? "flex-1" : "space-y-2")} noValidate data-testid="query-input">
      <div className="flex items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Input
            ref={ref}
            type="text"
            data-guide="query"
            value={draft}
            onChange={(event) => {
              setDraft(event.target.value);
              track(event.target);
              setAsked(true);
              setDismissed(false);
              setActive(-1);
            }}
            onKeyDown={onKeyDown}
            onKeyUp={(event) => track(event.currentTarget)}
            onClick={(event) => track(event.currentTarget)}
            onFocus={() => setFocused(true)}
            onBlur={() => {
              setFocused(false);
              setAsked(false);
              setActive(-1);
            }}
            aria-label={label}
            role="combobox"
            aria-expanded={open}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={active >= 0 ? `${listId}-${active}` : undefined}
            invalid={Boolean(failure)}
            autoFocus={autoFocus}
            spellCheck={false}
            autoComplete="off"
            placeholder="assignee = currentUser() AND statusCategory != done"
            data-query
            controlSize={compact ? "md" : "lg"}
            className="w-full min-w-0 font-mono text-sm"
          />
          {open && (
            <div id={listId} role="listbox" aria-label="Suggestions" data-query-suggestions className="absolute left-0 right-0 z-30 mt-1 max-h-80 overflow-y-auto rounded-overlay border border-border bg-surface-overlay p-1 shadow-2">
              {rows.map((row, i) => (
                <div
                  key={`${row.kind}:${row.text}:${row.detail ?? ""}`}
                  id={`${listId}-${i}`}
                  role="option"
                  aria-selected={i === active}
                  data-suggestion={row.kind}
                  data-suggestion-text={row.kind === "issue" ? row.issueKey : row.text}
                  onMouseDown={(event) => {
                    event.preventDefault();
                    choose(row);
                  }}
                  onMouseEnter={() => setActive(i)}
                  className={cx("flex cursor-pointer items-baseline gap-2 rounded-control px-2 py-1.5 text-sm", i === active ? "bg-surface-raised text-ink" : "text-ink-muted")}
                >
                  {row.kind === "issue" ? (
                    <>
                      <span className="shrink-0 font-mono text-xs text-ink-subtle">{row.detail}</span>
                      <span className="truncate text-ink">{row.text}</span>
                    </>
                  ) : (
                    <>
                      <span className="truncate font-mono text-ink">{row.text}</span>
                      {row.detail && <span className="shrink-0 text-xs text-ink-subtle">{row.detail}</span>}
                      {row.kind === "recent" && <span className="shrink-0 text-xs text-ink-subtle">recent</span>}
                    </>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
        {!compact && (
          <>
            <Popover
              open={help}
              onClose={() => setHelp(false)}
              label="What a query can say"
              align="end"
              className="max-h-[70vh] w-[64rem] max-w-[calc(100vw-20rem)] overflow-y-auto"
              trigger={<IconButton icon={<Icon.Info />} label="What a query can say" variant="secondary" size="lg" aria-expanded={help} onClick={() => setHelp((open) => !open)} data-action="query-help" />}
            >
              <QueryHelp
                onExample={(example) => {
                  setDraft(example);
                  setHelp(false);
                }}
              />
            </Popover>
            <Button type="submit" size="lg">
              Search
            </Button>
          </>
        )}
      </div>
      {failure && (
        <div className="text-sm" role="alert" data-query-error>
          <pre className="m-0 overflow-x-auto font-mono text-sm leading-tight text-ink">
            {value}
            {"\n"}
            <span className="text-danger" data-query-caret>
              {caretLine(failure.position)}
            </span>
          </pre>
          <p className="mt-1 text-danger">{failure.message}</p>
        </div>
      )}
    </form>
  );
}

/** What a query can say: the fields, the operators, the functions, a few examples. */
export function QueryHelp({ onExample }: { onExample?: (example: string) => void }) {
  return (
    <div className="text-sm text-ink-muted">
      <div className="grid gap-6 sm:grid-cols-[minmax(0,1fr)_minmax(0,22rem)]">
        <table className="min-w-0 text-left">
          <tbody>
            {QUERY_FIELDS.map((field) => (
              <tr key={field.name} className="align-top">
                <td className="pr-3 font-mono text-ink">{field.name}</td>
                <td className="pr-3">{field.takes}</td>
                <td className="font-mono text-ink-subtle">{field.example}</td>
              </tr>
            ))}
            <tr className="align-top">
              <td className="pr-3 font-mono text-ink">"Field name"</td>
              <td className="pr-3">a custom field, by its name</td>
              <td className="font-mono text-ink-subtle">{QUERY_CUSTOM_FIELD_EXAMPLE}</td>
            </tr>
          </tbody>
        </table>
        <div className="min-w-0 space-y-2 break-words">
          <p>
            <span className="text-ink">Operators</span> {QUERY_OPERATORS.join("  ")}
          </p>
          <p>
            <span className="text-ink">Functions</span> {QUERY_FUNCTIONS.join(", ")}; a date function takes a shift, as in startOfWeek(-1w).
          </p>
          <p>
            <span className="text-ink">Join</span> conditions with AND, OR, NOT and brackets; end with ORDER BY field DESC.
          </p>
          <p className="text-ink">For example</p>
          <ul className="space-y-1 font-mono text-ink-subtle [&_button]:whitespace-normal [&_button]:text-left">
            {QUERY_EXAMPLES.map((example) => (
              <li key={example}>
                <Button variant="link" className="font-mono" onClick={() => onExample?.(example)}>
                  {example}
                </Button>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}

/** Reads a query error out of whatever the request threw; anything else is not this input's to show. */
function describe(error: unknown): { message: string; position?: number } | null {
  if (error instanceof ApiError && error.code === "bad_query") {
    return { message: error.message, position: error.position };
  }
  return null;
}
