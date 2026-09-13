import type { ReactNode } from "react";

export function Detail({ label, wide = false, children }: { label: string; wide?: boolean; children: ReactNode }) {
  // A control that needs the width, such as a pair of dates, sits under its
  // label rather than fighting it for one line.
  if (wide) {
    return (
      <div className="py-2">
        <dt className="mb-1 text-sm text-ink-muted">{label}</dt>
        <dd className="flex min-w-0 justify-end text-sm">{children}</dd>
      </div>
    );
  }
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-x-3 py-2">
      <dt className="text-sm text-ink-muted">{label}</dt>
      <dd className="flex min-w-0 justify-end text-sm">{children}</dd>
    </div>
  );
}
