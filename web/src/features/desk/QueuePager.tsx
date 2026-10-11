import { Button } from "@/components/ui";

/** How many requests a whole queue holds, not just the page on screen. */
export function queueCount(total: number): string {
  return `${total} request${total === 1 ? "" : "s"}`;
}

/** The start of the last page, for a page left past the end when the queue shrank. */
export function clampQueueOffset(offset: number, total: number, pageSize: number): number {
  if (offset < total) return offset;
  return Math.max(0, Math.floor((total - 1) / pageSize) * pageSize);
}

interface QueuePagerProps {
  total: number;
  offset: number;
  pageSize: number;
  onChange: (offset: number) => void;
}

/** Steps through a queue longer than one page. */
export function QueuePager({ total, offset, pageSize, onChange }: QueuePagerProps) {
  if (total <= pageSize && offset === 0) return null;
  const last = Math.min(offset + pageSize, total);
  return (
    <nav aria-label="Queue pages" className="mt-3 flex items-center justify-end gap-2 text-sm text-ink-muted">
      <span className="tabular-nums" data-queue-range>
        {`${total === 0 ? 0 : offset + 1} to ${last} of ${total}`}
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
