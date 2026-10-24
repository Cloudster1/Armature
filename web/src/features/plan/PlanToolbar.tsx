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
import { t } from "@/i18n";

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
  onAutoPlan,
  autoPlanning = false,
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
  /** Asks for the auto plan's proposal; absent for someone who cannot move issues, or where nothing is drawn on dates. */
  onAutoPlan?: () => void;
  autoPlanning?: boolean;
}) {
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const active =
    filters.types.length +
    Number(Boolean(filters.milestoneId)) +
    Number(Boolean(filters.teamId)) +
    Number(closedForDays !== null && closedForDays !== 14 ? 1 : 0);

  function toggleType(name: string) {
    const types = filters.types.includes(name) ? filters.types.filter((each) => each !== name) : [...filters.types, name];
    onFilters({ ...filters, types });
  }

  return (
    <Toolbar
      label={t.plan.toolbar.controls}
      start={
        <>
          <Segmented<PlanView>
            label={t.plan.toolbar.view}
            value={view}
            options={PLAN_VIEWS.map((each) => ({ value: each.id, label: each.label, attrs: { title: each.hint } }))}
            onChange={onView}
          />
          <div className="flex min-w-64 max-w-xl flex-1 items-center gap-1" data-testid="plan-scope">
            <QueryInput value={query} onSubmit={onQuery} error={queryError} compact label={t.plan.toolbar.narrow} />
            <Popover
              open={helpOpen}
              onClose={() => setHelpOpen(false)}
              label={t.plan.toolbar.queryHelp}
              align="end"
              trigger={
                <Button
                  variant="ghost"
                  size="sm"
                  icon={<Icon.Help />}
                  onClick={() => setHelpOpen((o) => !o)}
                  aria-expanded={helpOpen}
                  aria-label={t.plan.toolbar.howToQuery}
                />
              }
              className="w-xl max-w-[90vw]"
            >
              <QueryHelp />
            </Popover>
          </div>
          <Popover
            open={filtersOpen}
            onClose={() => setFiltersOpen(false)}
            label={t.plan.toolbar.filters}
            trigger={
              <Button
                variant="secondary"
                icon={<Icon.Filter />}
                onClick={() => setFiltersOpen((o) => !o)}
                aria-expanded={filtersOpen}
                data-action="plan-filters"
              >
                {t.plan.toolbar.filters}
                {active > 0 && <Tag className="ml-1 bg-accent-subtle text-accent">{active}</Tag>}
              </Button>
            }
            className="w-80"
          >
            <div className="space-y-4" data-testid="plan-filters">
              <div>
                <p className="mb-1.5 text-2xs font-medium tracking-wide text-ink-subtle uppercase">{t.plan.toolbar.type}</p>
                <div className="flex flex-wrap gap-1" role="group" aria-label={t.plan.toolbar.filterByType}>
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
                <Select
                  label={t.plan.toolbar.milestone}
                  aria-label={t.plan.toolbar.filterByMilestone}
                  id="plan-filter-milestone"
                  value={filters.milestoneId}
                  onChange={(event) => onFilters({ ...filters, milestoneId: event.target.value })}
                >
                  <option value="">{t.plan.toolbar.anyMilestone}</option>
                  {milestones.map((milestone) => (
                    <option key={milestone.id} value={milestone.id}>
                      {milestone.name}
                    </option>
                  ))}
                </Select>
              )}
              {teams.length > 0 && (
                <Select
                  label={t.plan.toolbar.team}
                  aria-label={t.plan.toolbar.filterByTeam}
                  id="plan-filter-team"
                  value={filters.teamId}
                  onChange={(event) => onFilters({ ...filters, teamId: event.target.value })}
                >
                  <option value="">{t.plan.toolbar.anyTeam}</option>
                  {teams.map((team) => (
                    <option key={team.id} value={team.id}>
                      {team.name}
                    </option>
                  ))}
                </Select>
              )}
              <div>
                <label htmlFor="plan-closed-for" className="block text-sm font-medium text-ink-muted">
                  {t.plan.toolbar.hideDone}
                </label>
                <div className="mt-1 flex items-center gap-2">
                  <Input
                    id="plan-closed-for"
                    type="number"
                    min={0}
                    step={1}
                    inputMode="numeric"
                    aria-label={t.plan.toolbar.hideDone}
                    data-plan-closed-for
                    value={closedForDays ?? ""}
                    placeholder={t.plan.toolbar.any}
                    onChange={(event) => {
                      const text = event.target.value.trim();
                      onClosedForDays(text === "" ? null : Math.max(0, Math.floor(Number(text))));
                    }}
                    className="max-w-20 text-right tabular-nums"
                  />
                  <span className="text-sm text-ink-muted">{t.plan.toolbar.days}</span>
                </div>
                <p className="mt-1 text-xs text-ink-subtle">
                  {closedForDays === null ? t.plan.toolbar.keepAllDone : closedForDays === 0 ? t.plan.toolbar.noDone : t.plan.toolbar.blankKeepsAll}
                </p>
              </div>
              {(filters.types.length > 0 || filters.milestoneId || filters.teamId) && (
                <Button size="sm" variant="ghost" onClick={() => onFilters({ types: [], milestoneId: "", teamId: "" })}>
                  {t.plan.toolbar.clearFilters}
                </Button>
              )}
            </div>
          </Popover>
          {onAutoPlan && (
            <Button
              variant="secondary"
              icon={<Icon.Calendar />}
              onClick={onAutoPlan}
              disabled={autoPlanning}
              title={t.plan.toolbar.autoPlanHint}
              data-action="auto-plan"
            >
              {autoPlanning ? t.plan.toolbar.planning : t.plan.toolbar.autoPlan}
            </Button>
          )}
        </>
      }
      end={
        showZoom ? (
          <Segmented<Zoom["id"]>
            label={t.plan.zoom.label}
            value={zoom}
            options={ZOOMS.map((option) => ({ value: option.id, label: option.label }))}
            onChange={onZoom}
          />
        ) : undefined
      }
    />
  );
}

/** The numbers over the whole plan as one line, with the breakdown behind a disclosure. */
export function PlanSummary({ numbers, children }: { numbers: Meter; children?: React.ReactNode }) {
  const [more, setMore] = useState(false);
  return (
    <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm" data-testid="plan-meter">
      <dl className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
        <Figure label={t.plan.summary.issues} value={numbers.total} />
        <Figure label={t.plan.summary.scheduled} value={numbers.scheduled} />
        <Figure label={t.plan.summary.todo} value={numbers.todo} />
        <Figure label={t.plan.summary.inProgress} value={numbers.inProgress} />
        <Figure label={t.plan.summary.done} value={`${numbers.done} (${numbers.donePercent}%)`} />
        <Figure label={t.plan.summary.points} value={number(numbers.points)} />
      </dl>
      {children}
      <Button
        variant="link"
        onClick={() => setMore((m) => !m)}
        aria-expanded={more}
        className="ml-auto text-xs"
        icon={more ? <Icon.ChevronUp /> : <Icon.ChevronDown />}
        data-plan-more
      >
        {more ? t.plan.summary.less : t.plan.summary.more}
      </Button>
      {more && (
        <div className="basis-full text-xs text-ink-muted">{numbers.byType.map((type) => t.plan.summary.ofType(type.count, type.name)).join(" · ")}</div>
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
