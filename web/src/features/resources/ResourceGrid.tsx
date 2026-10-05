import { useState } from "react";
import { Link } from "@tanstack/react-router";
import type { ResourcePlan, ResourceRow, ResourceWeek } from "@/api/resources";
import { Choice, Popover } from "@/components/ui";
import { cx } from "@/components/ui/cx";
import { RESOURCE_BAR_HEIGHT, RESOURCE_WEEK_MIN_PX } from "@/config";
import type { Utilisation } from "@/features/plan/load";
import { number } from "@/features/plan/SprintBands";
import { cellShares, cellState, describeCell, describeHours, weekLabel } from "./cells";

const tone: Record<Utilisation, string> = {
  unmeasured: "bg-status-todo/70",
  under: "bg-status-done/70",
  full: "bg-accent/70",
  over: "bg-danger/80",
};

/** A row per team or person and a column per week; a cell opens the issues of its week. */
export function ResourceGrid({ plan }: { plan: ResourcePlan }) {
  const [open, setOpen] = useState<string | null>(null);
  const columns = `minmax(9rem, 14rem) repeat(${plan.weeks.length}, minmax(${RESOURCE_WEEK_MIN_PX}px, 1fr))`;
  return (
    <div className="overflow-x-auto rounded-control border border-border bg-surface" data-resources={plan.grouping}>
      <div role="grid" aria-label="Hours per week" className="grid min-w-max text-xs" style={{ gridTemplateColumns: columns }}>
        <div role="row" className="contents">
          <div role="columnheader" className="sticky left-0 z-10 bg-surface px-3 py-2 font-medium text-ink-muted">
            {plan.grouping === "person" ? "Person" : "Team"}
          </div>
          {plan.weeks.map((start) => (
            <div key={start} role="columnheader" className="border-l border-border px-2 py-2 font-medium text-ink-muted tabular-nums" data-resource-column={start.slice(0, 10)}>
              {weekLabel(start)}
            </div>
          ))}
        </div>
        {plan.rows.map((row) => (
          <div key={row.id ?? row.kind} role="row" className="contents" data-resource-row={row.name} data-resource-kind={row.kind}>
            <div role="rowheader" className={cx("sticky left-0 z-10 flex items-center border-t border-border bg-surface px-3 text-sm", row.kind === "unassigned" ? "text-ink-muted" : "text-ink")}>
              <span className="truncate">{row.name}</span>
            </div>
            {row.weeks.map((week) => {
              const key = `${row.id ?? row.kind}:${week.start}`;
              return <Cell key={key} row={row} week={week} open={open === key} onToggle={() => setOpen(open === key ? null : key)} onClose={() => setOpen(null)} />;
            })}
          </div>
        ))}
      </div>
    </div>
  );
}

function Cell({ row, week, open, onToggle, onClose }: { row: ResourceRow; week: ResourceWeek; open: boolean; onToggle: () => void; onClose: () => void }) {
  const state = cellState(week);
  const shares = cellShares(row, week);
  const title = describeCell(row, week);
  const px = (share: number) => Math.round(share * RESOURCE_BAR_HEIGHT);
  const daysOff = shares.capacity !== undefined && shares.nominal !== undefined && shares.nominal > shares.capacity;
  return (
    // The popover's own wrappers are inline; the cell stretches them to its width.
    <div role="gridcell" className="border-t border-l border-border p-1 [&>div]:flex [&>div>div]:flex-1">
      <Popover
        open={open}
        onClose={onClose}
        label={`${row.name}, week of ${weekLabel(week.start)}`}
        trigger={
          <Choice
            onSelect={onToggle}
            title={title}
            aria-label={title}
            aria-expanded={open}
            data-resource-week={week.start.slice(0, 10)}
            data-load={week.loadHours}
            data-capacity={week.capacityHours ?? ""}
            data-days-away={week.daysAway ?? 0}
            data-holidays={week.holidays ?? 0}
            data-over={state === "over" ? "true" : "false"}
            className="flex w-full flex-col gap-1 px-1 pt-0.5 hover:bg-surface-raised"
          >
            <span className={cx("text-2xs tabular-nums", state === "over" ? "font-medium text-danger" : "text-ink-muted")}>
              {week.capacityHours !== undefined ? `${number(week.loadHours)} / ${number(week.capacityHours)} h` : `${number(week.loadHours)} h`}
            </span>
            <span aria-hidden="true" className="relative block w-full" style={{ height: RESOURCE_BAR_HEIGHT }}>
              {daysOff && (
                <span className="absolute inset-x-0 bg-hatched" data-days-off style={{ bottom: px(shares.capacity!), height: px(shares.nominal! - shares.capacity!) }} />
              )}
              {shares.capacity !== undefined && <span className="absolute inset-x-0 border-t border-dashed border-border-strong" style={{ bottom: px(shares.capacity) }} />}
              <span className={cx("absolute inset-x-1 bottom-0 rounded-t-sm", tone[state])} style={{ height: Math.max(week.loadHours > 0 ? 2 : 0, px(shares.load)) }} />
            </span>
          </Choice>
        }
      >
        <WeekIssues row={row} week={week} />
      </Popover>
    </div>
  );
}

/** The issues that make up a week, with their hours in it. */
export function WeekIssues({ row, week }: { row: ResourceRow; week: ResourceWeek }) {
  return (
    <div className="max-w-sm space-y-2 text-sm" data-resource-panel={`${row.name}|${week.start.slice(0, 10)}`}>
      <div>
        <p className="font-medium text-ink">
          {row.name}, week of {weekLabel(week.start)}
        </p>
        <p className="text-xs text-ink-muted">{describeHours(week)}</p>
      </div>
      {week.issues.length === 0 ? (
        <p className="text-xs text-ink-subtle">Nothing is scheduled this week.</p>
      ) : (
        <ul className="space-y-1">
          {week.issues.map((issue) => (
            <li key={issue.key} className="flex items-baseline justify-between gap-3" data-resource-issue={issue.key}>
              <Link to="/issues/$issueKey" params={{ issueKey: issue.key }} className="min-w-0 truncate text-ink hover:text-accent">
                <span className="font-mono text-xs text-ink-muted">{issue.key}</span> {issue.summary}
              </Link>
              <span className="shrink-0 text-xs text-ink-muted tabular-nums">{number(issue.hours)} h</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
