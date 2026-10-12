import { Button } from "./Button";

/** How many a whole list holds, not just the page on screen. */
export function countOf(total: number, noun: string): string {
  return `${total} ${noun}${total === 1 ? "" : "s"}`;
}

function range(offset: number, shown: number, total: number): string {
  return `${total === 0 ? 0 : offset + 1} to ${offset + shown} of ${total}`;
}

/** The whole count while it fits on the page, and which of it is shown once it does not. */
export function shownOf(total: number, offset: number, shown: number, noun: string): string {
  if (offset === 0 && shown >= total) return countOf(total, noun);
  return `${range(offset, shown, total)} ${noun}s`;
}

/** A page past the first, as an address names it; the first page is the address without one. */
export function isPageOffset(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value > 0;
}

/** The start of the last page, for a page left past the end when the list shrank. */
export function clampOffset(offset: number, total: number, pageSize: number): number {
  if (offset < total) return offset;
  return Math.max(0, Math.floor((total - 1) / pageSize) * pageSize);
}

interface PagerProps {
  total: number;
  offset: number;
  pageSize: number;
  onChange: (offset: number) => void;
  /** Names the list for a screen reader, as "Queue pages" or "Issue pages". */
  label: string;
}

/** Steps through a list longer than one page. */
export function Pager({ total, offset, pageSize, onChange, label }: PagerProps) {
  if (total <= pageSize && offset === 0) return null;
  return (
    <nav aria-label={label} className="mt-3 flex items-center justify-end gap-2 text-sm text-ink-muted">
      <span className="tabular-nums" data-page-range>
        {range(offset, Math.max(0, Math.min(pageSize, total - offset)), total)}
      </span>
      <Button variant="ghost" size="sm" aria-label="Previous page" disabled={offset === 0} onClick={() => onChange(Math.max(0, offset - pageSize))}>
        Previous
      </Button>
      <Button variant="ghost" size="sm" aria-label="Next page" disabled={offset + pageSize >= total} onClick={() => onChange(offset + pageSize)}>
        Next
      </Button>
    </nav>
  );
}
