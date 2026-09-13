import type { ReactNode, RefObject } from "react";

const railStops = [
  { id: "description", label: "Description" },
  { id: "activity", label: "Activity" },
  { id: "attachments", label: "Attachments" },
  { id: "work", label: "Work" },
  { id: "development", label: "Development" },
  { id: "children", label: "Children" },
  { id: "links", label: "Links" },
  { id: "watchers", label: "Watchers" },
  { id: "history", label: "History" },
];

// The rail jumps within its own page, found from the root rather than by a
// global id, so a page and a panel showing two issues never cross wires.
export function Rail({ root }: { root: RefObject<HTMLDivElement | null> }) {
  function jump(event: React.MouseEvent<HTMLAnchorElement>, id: string) {
    event.preventDefault();
    root.current?.querySelector(`[data-section="${id}"]`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  }
  return (
    <nav aria-label="On this page" className="mb-5 flex flex-wrap gap-1 border-b border-border text-sm">
      {railStops.map((stop) => (
        <a key={stop.id} href={`#issue-${stop.id}`} onClick={(e) => jump(e, stop.id)} className="-mb-px border-b-2 border-transparent px-2 py-1.5 text-ink-muted hover:border-border-strong hover:text-ink" data-rail={stop.id}>
          {stop.label}
        </a>
      ))}
    </nav>
  );
}

/** A section the rail can reach; a hidden title leaves the panel's own heading in charge. */
export function Section({ id, title, hidden, children }: { id: string; title?: string; hidden?: boolean; children: ReactNode }) {
  return (
    <section data-section={id} className="scroll-mt-24" aria-label={title}>
      {title && !hidden && <h2 className="mb-3 text-xs font-semibold tracking-wide text-ink-muted uppercase">{title}</h2>}
      {children}
    </section>
  );
}
