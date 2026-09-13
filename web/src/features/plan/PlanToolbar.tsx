import { useState } from "react";
import type { Milestone } from "@/api/milestones";
import { Button, Chip, Input, Popover, Segmented, Select, Tag, Toolbar } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TypeBadge } from "@/features/issues/badges";
import { QueryInput } from "@/features/search/QueryInput";
import { QueryHelp } from "@/features/search/QueryInput";
import { ZOOMS, type Zoom } from "./scale";
import { number } from "./SprintBands";
import { PLAN_VIEWS, type Filters, type Meter, type PlanView } from "./views";

// One toolbar over the plan. The view, the query, the filters and the zoom
// sit in one row; the cut and the filters are one popover, because to a
// reader they are one question: what is drawn.
export function PlanToolbar({
  view,
  onView,
  query,
  onQuery,
  queryError,
  filters,
  onFilters,
  closedForDays,
  onClosedForDays,
  numbers,
  milestones,
  teams,
  zoom,
  onZoom,
  showZoom,
}: {
  view: PlanView;
  onView: (view: PlanView) => void;
  query: string;
  onQuery: (query: string) => void;
  queryError?: unknown;
  filters: Filters;
  onFilters: (filters: Filters) => void;
  closedForDays: number | null;
  onClosedForDays: (days: number | null) => void;
  numbers: Meter;
  milestones: Milestone[];
  teams: Array<{ id: string; name: string }>;
  zoom: Zoom["id"];
  onZoom: (zoom: Zoom["id"]) => void;
  showZoom: boolean;
}) {
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const active = filters.types.length + Number(Boolean(filters.milestoneId)) + Number(Boolean(filters.teamId)) + Number(closedForDays !== null && closedForDays !== 14 ? 1 : 0);

  function toggleType(name: string) {
    const types = filters.types.includes(name) ? filters.types.filter((each) => each !== name) : [...filters.types, name];
    onFilters({ ...filters, types });
  }

  return (
    <Toolbar
      label="Plan controls"
      start={
        <>
          <Segmented<PlanView> label="Plan view" value={view} options={PLAN_VIEWS.map((each) => ({ value: each.id, label: each.label, attrs: { title: each.hint } }))} onChange={onView} />
          <div className="flex min-w-64 max-w-xl flex-1 items-center gap-1" data-testid="plan-scope">
            <QueryInput value={query} onSubmit={onQuery} error={queryError} compact label="Narrow the plan with a query" />
            <Popover
              open={helpOpen}
              onClose={() => setHelpOpen(false)}
              label="Query help"
              align="end"
              trigger={<Button variant="ghost" size="sm" icon={<Icon.Help />} onClick={() => setHelpOpen((o) => !o)} aria-expanded={helpOpen} aria-label="How to write a query" />}
              className="w-[36rem] max-w-[90vw]"
            >
              <QueryHelp />
            </Popover>
          </div>
          <Popover
            open={filtersOpen}
            onClose={() => setFiltersOpen(false)}
            label="Filters"
            trigger={
              <Button variant="secondary" icon={<Icon.Filter />} onClick={() => setFiltersOpen((o) => !o)} aria-expanded={filtersOpen} data-action="plan-filters">
                Filters{active > 0 && <Tag className="ml-1 bg-accent-subtle text-accent">{active}</Tag>}
              </Button>
            }
            className="w-80"
          >
            <div className="space-y-4" data-testid="plan-filters">
              <div>
                <p className="mb-1.5 text-2xs font-medium tracking-wide text-ink-subtle uppercase">Type</p>
                <div className="flex flex-wrap gap-1" role="group" aria-label="Filter by type">
                  {numbers.byType.map((type) => {
                    const on = filters.types.includes(type.name);
                    return (
                      <Chip key={type.name} pressed={on} data-plan-type-filter={type.name} onClick={() => toggleType(type.name)}>
                        <TypeBadge icon={type.icon} name={type.name} />
                        {type.name}
                        <span className="tabular-nums opacity-70">{type.count}</span>
                      </Chip>
                    );
                  })}
                </div>
              </div>
              {milestones.length > 0 && (
                <Select label="Milestone" aria-label="Filter by milestone" id="plan-filter-milestone" value={filters.milestoneId} onChange={(event) => onFilters({ ...filters, milestoneId: event.target.value })}>
                  <option value="">Any milestone</option>
                  {milestones.map((milestone) => (
                    <option key={milestone.id} value={milestone.id}>
                      {milestone.name}
                    </option>
                  ))}
                </Select>
              )}
              {teams.length > 0 && (
                <Select label="Team" aria-label="Filter by team" id="plan-filter-team" value={filters.teamId} onChange={(event) => onFilters({ ...filters, teamId: event.target.value })}>
                  <option value="">Any team</option>
                  {teams.map((team) => (
                    <option key={team.id} value={team.id}>
                      {team.name}
                    </option>
                  ))}
                </Select>
              )}
              <div>
                <label htmlFor="plan-closed-for" className="block text-sm font-medium text-ink-muted">
                  Hide tickets done for more than
                </label>
                <div className="mt-1 flex items-center gap-2">
                  <Input
                    id="plan-closed-for"
                    type="number"
                    min={0}
                    step={1}
                    inputMode="numeric"
                    aria-label="Hide tickets done for more than"
                    data-plan-closed-for
                    value={closedForDays ?? ""}
                    placeholder="any"
                    onChange={(event) => {
                      const text = event.target.value.trim();
                      onClosedForDays(text === "" ? null : Math.max(0, Math.floor(Number(text))));
                    }}
                    className="max-w-20 text-right tabular-nums"
                  />
                  <span className="text-sm text-ink-muted">days</span>
                </div>
                <p className="mt-1 text-xs text-ink-subtle">
                  {closedForDays === null ? "Everything done stays on the plan." : closedForDays === 0 ? "Done work is not drawn." : "Leave it blank to keep everything."}
                </p>
              </div>
              {(filters.types.length > 0 || filters.milestoneId || filters.teamId) && (
                <Button size="sm" variant="ghost" onClick={() => onFilters({ types: [], milestoneId: "", teamId: "" })}>
                  Clear filters
                </Button>
              )}
            </div>
          </Popover>
        </>
      }
      end={showZoom ? <Segmented<Zoom["id"]> label="Zoom" value={zoom} options={ZOOMS.map((option) => ({ value: option.id, label: option.label }))} onChange={onZoom} /> : undefined}
    />
  );
}

/** The numbers over the whole plan as one line, with the breakdown behind a disclosure. */
export function PlanSummary({ numbers, children }: { numbers: Meter; children?: React.ReactNode }) {
  const [more, setMore] = useState(false);
  return (
    <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm" data-testid="plan-meter">
      <dl className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
        <Figure label="issues" value={numbers.total} />
        <Figure label="scheduled" value={numbers.scheduled} />
        <Figure label="to do" value={numbers.todo} />
        <Figure label="in progress" value={numbers.inProgress} />
        <Figure label="done" value={`${numbers.done} (${numbers.donePercent}%)`} />
        <Figure label="points" value={number(numbers.points)} />
      </dl>
      {children}
      <Button variant="link" onClick={() => setMore((m) => !m)} aria-expanded={more} className="ml-auto text-xs" icon={more ? <Icon.ChevronUp /> : <Icon.ChevronDown />} data-plan-more>
        {more ? "Less" : "More"}
      </Button>
      {more && (
        <div className="basis-full text-xs text-ink-muted">
          {numbers.byType.map((type) => `${type.count} ${type.name.toLowerCase()}${type.count === 1 ? "" : "s"}`).join(" · ")}
        </div>
      )}
    </div>
  );
}

function Figure({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="flex items-baseline gap-1.5" data-plan-figure={label}>
      <dd className="font-semibold text-ink tabular-nums">{value}</dd>
      <dt className="text-ink-muted">{label}</dt>
    </div>
  );
}
