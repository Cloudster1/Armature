import { useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  hours,
  share,
  useReport,
  useReportSource,
  type Breakdown,
  type BurndownReport,
  type CycleTimeReport,
  type EpicsReport,
  type SLAReport,
  type SprintBurndown,
  type SprintHistoryReport,
  type SprintReport,
  type ThroughputReport,
  type VelocityReport,
  type WorkloadReport,
} from "@/api/reports";
import { Button } from "@/components/ui";
import { BarList, LineChart, PairedBars, Progress, Stat, weekLabel } from "./charts";
import { axisLabels, burndownSeries, describeScope } from "./burndown";
import { ChartBody } from "./ChartBody";
import { MilestonesBody } from "./MilestonesBody";
import {
  AverageAgeBody,
  CSATBody,
  ControlChartBody,
  CreatedResolvedBody,
  CumulativeFlowBody,
  ReleaseBurndownBody,
  ResolutionBody,
  VersionsBody,
} from "./FlowBodies";
import { useWidgetReport, type BodyProps } from "./useWidgetReport";
import { WidgetState } from "./WidgetState";
import { t } from "@/i18n";

const categoryTone: Record<string, string> = {
  todo: "bg-status-todo",
  in_progress: "bg-status-progress",
  done: "bg-status-done",
};

/** Draws one widget's report. Each kind knows how it reads best. */
export function WidgetBody({ projectKey, widget, narrow }: BodyProps) {
  const props = { projectKey, widget, narrow };
  switch (widget.kind) {
    case "status_breakdown":
    case "priority_breakdown":
    case "type_breakdown":
    case "request_types":
      return <BreakdownBody {...props} />;
    case "throughput":
      return <ThroughputBody {...props} />;
    case "workload":
    case "team_workload":
      return <WorkloadBody {...props} />;
    case "cycle_time":
      return <CycleTimeBody {...props} />;
    case "epics":
      return <EpicsBody {...props} />;
    case "sprint":
      return <SprintBody {...props} />;
    case "velocity":
      return <VelocityBody {...props} />;
    case "burndown":
      return <BurndownBody {...props} />;
    case "sprint_history":
      return <SprintHistoryBody {...props} />;
    case "sla":
      return <SLABody {...props} />;
    case "filter":
      return null;
    case "chart":
      return <ChartBody {...props} />;
    case "milestones":
      return <MilestonesBody {...props} />;
    case "versions":
      return <VersionsBody {...props} />;
    case "cumulative_flow":
      return <CumulativeFlowBody {...props} />;
    case "control_chart":
      return <ControlChartBody {...props} />;
    case "created_vs_resolved":
      return <CreatedResolvedBody {...props} />;
    case "avg_age":
      return <AverageAgeBody {...props} />;
    case "resolution_histogram":
      return <ResolutionBody {...props} />;
    case "release_burndown":
      return <ReleaseBurndownBody {...props} />;
    case "csat":
      return <CSATBody {...props} />;
  }
}

function BreakdownBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<Breakdown>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  return (
    <div>
      <BarList
        rows={data.buckets.map((b) => ({ label: b.label, count: b.count, key: b.label + b.category }))}
        tone={(row) => categoryTone[data.buckets.find((b) => b.label === row.label)?.category ?? ""] ?? "bg-accent"}
        empty={t.dashboard.widgets.noIssues}
      />
      <p className="mt-2 text-2xs text-ink-subtle">{t.dashboard.widgets.inAll(data.total)}</p>
    </div>
  );
}

function ThroughputBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<ThroughputReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  return (
    <div>
      <div className="mb-3 flex gap-6">
        <Stat value={String(data.created)} label={t.dashboard.widgets.createdIn(data.days)} />
        <Stat value={String(data.resolved)} label={t.dashboard.widgets.resolved} />
      </div>
      <PairedBars
        groups={data.weeks.map((w) => ({ label: weekLabel(w.start), a: w.created, b: w.resolved }))}
        a={{ name: t.dashboard.widgets.created, tone: "bg-status-todo" }}
        b={{ name: t.dashboard.widgets.resolved, tone: "bg-status-done" }}
      />
    </div>
  );
}

function WorkloadBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<WorkloadReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.rows.length === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.nobodyCarrying}</p>;
  return (
    <table className="w-full text-sm" data-testid="workload">
      <thead>
        <tr className="text-left text-2xs tracking-wide text-ink-subtle uppercase">
          <th className="pb-1 font-medium">{t.dashboard.widgets.who}</th>
          <th className="pb-1 text-right font-medium">{t.dashboard.widgets.open}</th>
          <th className="pb-1 text-right font-medium">{t.dashboard.widgets.doing}</th>
          <th className="pb-1 text-right font-medium">{t.dashboard.widgets.points}</th>
          <th className="pb-1 text-right font-medium">{t.dashboard.widgets.doneIn(data.days)}</th>
        </tr>
      </thead>
      <tbody>
        {data.rows.map((row) => (
          <tr key={row.id ?? row.name} className="border-t border-border" data-load={row.name}>
            <td className="py-1 text-ink">{row.name}</td>
            <td className="py-1 text-right text-ink tabular-nums">{row.open}</td>
            <td className="py-1 text-right text-ink-muted tabular-nums">{row.inProgress}</td>
            <td className="py-1 text-right text-ink-muted tabular-nums">{row.points || ""}</td>
            <td className="py-1 text-right text-ink-muted tabular-nums">{row.doneRecently}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function CycleTimeBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<CycleTimeReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.resolved === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.nothingResolved(data.days)}</p>;
  return (
    <div>
      <div className="flex gap-6">
        <Stat value={hours(data.medianHours)} label={t.dashboard.widgets.median} />
        <Stat value={hours(data.averageHours)} label={t.dashboard.widgets.average} />
        <Stat value={hours(data.p90Hours)} label={t.dashboard.widgets.slowestTenth} />
      </div>
      <p className="mt-2 text-2xs text-ink-subtle">{t.dashboard.widgets.resolvedIn(data.resolved, data.days)}</p>
    </div>
  );
}

function EpicsBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<EpicsReport>(projectKey, widget, narrow);
  const { readOnly } = useReportSource();
  if (!data) return <WidgetState error={error} />;
  if (data.epics.length === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.noEpics}</p>;
  return (
    <ul className="space-y-2">
      {data.epics.map((e) => (
        <li key={e.key} data-epic={e.key}>
          <div className="flex items-baseline justify-between gap-2 text-sm">
            {readOnly ? (
              <span className="min-w-0 truncate text-ink">{e.summary}</span>
            ) : (
              <Link to="/issues/$issueKey" params={{ issueKey: e.key }} className="min-w-0 truncate text-ink hover:text-accent">
                {e.summary}
              </Link>
            )}
            <span className="shrink-0 font-mono text-2xs text-ink-subtle">{e.key}</span>
          </div>
          <Progress done={e.done} total={e.total} label={e.total ? `${share(e.done, e.total)}%` : t.dashboard.widgets.empty} />
        </li>
      ))}
    </ul>
  );
}

function SprintBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<SprintReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (!data.active) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.noSprintRunning}</p>;
  return (
    <div className="space-y-3">
      {data.plans.map((p) => (
        <div key={p.sprint.id} data-sprint-standing={p.sprint.name}>
          <div className="flex items-baseline justify-between text-sm">
            <span className="text-ink">
              {p.sprint.name}
              {p.sprint.teamName && <span className="text-ink-muted"> · {p.sprint.teamName}</span>}
            </span>
            <span className="text-ink-muted">{t.dashboard.widgets.daysLeft(p.daysLeft)}</span>
          </div>
          <Progress done={p.completed} total={p.committed} label={t.dashboard.widgets.sprintPoints(p.completed, p.committed)} />
          <p className="mt-1 text-2xs text-ink-subtle">
            {t.dashboard.widgets.issueCount(p.issues)}
            {p.unestimated > 0 ? t.dashboard.widgets.unestimated(p.unestimated) : ""}
            {p.sprint.capacity ? t.dashboard.widgets.capacity(p.sprint.capacity) : ""}
          </p>
        </div>
      ))}
    </div>
  );
}

function VelocityBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<VelocityReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.sprints.length === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.noSprintCompleted}</p>;
  return (
    <PairedBars
      groups={data.sprints.map((s) => ({ label: s.name, a: s.committed, b: s.completed }))}
      a={{ name: t.dashboard.widgets.committed, tone: "bg-status-todo" }}
      b={{ name: t.dashboard.widgets.completed, tone: "bg-status-done" }}
    />
  );
}

function BurndownBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<BurndownReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.sprints.length === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.noSprintRunning}</p>;
  return (
    <div className="space-y-4">
      {data.sprints.map((curve) => (
        <BurndownChart key={curve.sprint.id} curve={curve} />
      ))}
    </div>
  );
}

function BurndownChart({ curve }: { curve: SprintBurndown }) {
  return (
    <div data-burndown={curve.sprint.name}>
      <div className="mb-1 flex items-baseline justify-between text-sm">
        <span className="text-ink">
          {curve.sprint.name}
          {curve.sprint.teamName && <span className="text-ink-muted"> · {curve.sprint.teamName}</span>}
        </span>
        {curve.live && <span className="text-2xs text-ink-subtle">{t.dashboard.widgets.todayLive}</span>}
      </div>
      <LineChart series={burndownSeries(curve)} xLabels={axisLabels(curve)} />
      <p className="mt-1 text-2xs text-ink-subtle" data-burndown-summary>
        {describeScope(curve)}
      </p>
    </div>
  );
}

function SprintHistoryBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<SprintHistoryReport>(projectKey, widget, narrow);
  // A shared link answers only for its widgets, so it has no curve to give a visitor.
  const { readOnly } = useReportSource();
  const [curveOf, setCurveOf] = useState<string | null>(null);
  if (!data) return <WidgetState error={error} />;
  if (data.sprints.length === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.noSprintCompleted}</p>;
  return (
    <div className="space-y-2">
      <table className="w-full text-sm">
        <thead className="text-2xs text-ink-muted">
          <tr>
            <th className="py-1 text-left font-medium">{t.dashboard.widgets.sprint}</th>
            <th className="py-1 text-right font-medium">{t.dashboard.widgets.committedHead}</th>
            <th className="py-1 text-right font-medium">{t.dashboard.widgets.completedHead}</th>
            <th className="py-1 text-right font-medium">{t.dashboard.widgets.issuesHead}</th>
            {!readOnly && <th className="py-1"></th>}
          </tr>
        </thead>
        <tbody>
          {data.sprints.map((s) => (
            <tr key={s.id} className="border-t border-border" data-sprint-outcome={s.name}>
              <td className="py-1.5 text-ink">
                {s.name}
                {s.team && <span className="text-ink-muted"> · {s.team}</span>}
              </td>
              <td className="py-1.5 text-right text-ink-muted tabular-nums">{t.dashboard.widgets.pts(s.committed)}</td>
              <td className="py-1.5 text-right text-ink tabular-nums">{t.dashboard.widgets.pts(s.completed)}</td>
              <td className="py-1.5 text-right text-ink-muted tabular-nums">{t.dashboard.widgets.finishedCarried(s.finished, s.carried)}</td>
              {!readOnly && (
                <td className="py-1.5 text-right">
                  <Button size="sm" variant="ghost" onClick={() => setCurveOf(curveOf === s.id ? null : s.id)}>
                    {curveOf === s.id ? t.dashboard.widgets.hide : t.dashboard.widgets.curve}
                  </Button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {curveOf && !readOnly && <PastBurndown projectKey={projectKey} sprintId={curveOf} />}
    </div>
  );
}

/** A finished sprint's curve, fetched when asked for. */
function PastBurndown({ projectKey, sprintId }: { projectKey: string; sprintId: string }) {
  const { data, error } = useReport<BurndownReport>(projectKey, "burndown", { sprintId });
  if (!data) return <WidgetState error={error} />;
  const curve = data.sprints[0];
  if (!curve) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.nothingWritten}</p>;
  return <BurndownChart curve={curve} />;
}

function SLABody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<SLAReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.metrics.length === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.noGoals(data.days)}</p>;
  return (
    <div className="space-y-3">
      {data.metrics.map((m) => {
        const decided = m.met + m.breached;
        return (
          <div key={m.metric} data-sla-metric={m.metric}>
            <div className="flex items-baseline justify-between text-sm">
              <span className="text-ink">{m.name}</span>
              <span className="text-ink-muted">
                {decided ? t.dashboard.widgets.metShare(share(m.met, decided)) : t.dashboard.widgets.nothingDecided}
                {m.running ? t.dashboard.widgets.running(m.running) : ""}
              </span>
            </div>
            <div className="mt-1 flex h-2 overflow-hidden rounded-sm bg-surface-raised">
              <span className="bg-status-done" style={{ width: `${share(m.met, Math.max(decided, 1))}%` }} />
              <span className="bg-danger" style={{ width: `${share(m.breached, Math.max(decided, 1))}%` }} />
            </div>
            <p className="mt-1 text-2xs text-ink-subtle">
              {t.dashboard.widgets.metMissed(m.met, m.breached)}
              {m.averageHours ? t.dashboard.widgets.onAverage(hours(m.averageHours)) : ""}
            </p>
          </div>
        );
      })}
    </div>
  );
}
