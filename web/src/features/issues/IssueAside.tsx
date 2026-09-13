import { Fragment, type ReactNode } from "react";
import type { Placement } from "@/api/arrange";
import type { Issue } from "@/api/issues";
import { Icon } from "@/components/icons";
import { ASIDE_AREAS, keyOf, namedFields, placesIn } from "./view/areas";
import { renderPlace } from "./view/slots";

/** The facts about an issue, in the groups and the order the project arranged. */
export function IssueAside({ issue, editable, places }: { issue: Issue; editable: boolean; places: Placement[] }) {
  const named = namedFields(places);
  return (
    <aside>
      {ASIDE_AREAS.map(({ area, title }) => {
        const inArea = placesIn(places, area);
        if (inArea.length === 0) return null;
        return (
          <Group key={area} title={title} open>
            {inArea.map((place) => (
              <Fragment key={keyOf(place)}>{renderPlace(place, { issue, editable, named })}</Fragment>
            ))}
          </Group>
        );
      })}
    </aside>
  );
}

/** A group of facts that folds, so a reader keeps what they care about open. */
function Group({ title, open, children }: { title: string; open?: boolean; children: ReactNode }) {
  return (
    <details open={open} className="group mb-3 rounded-overlay border border-border bg-surface" data-issue-group={title}>
      <summary className="flex cursor-pointer items-center gap-2 px-4 py-2 text-2xs font-medium tracking-wide text-ink-subtle uppercase select-none [&::-webkit-details-marker]:hidden">
        <Icon.ChevronRight className="transition-transform group-open:rotate-90" />
        {title}
      </summary>
      <dl className="divide-y divide-border border-t border-border px-4 text-sm">{children}</dl>
    </details>
  );
}
