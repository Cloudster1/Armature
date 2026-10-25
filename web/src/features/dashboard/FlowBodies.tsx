import { Link } from "@tanstack/react-router";
import type {
  AverageAgeReport,
  CSATReport,
  ControlChartReport,
  CreatedResolvedReport,
  CumulativeFlowReport,
  ReleaseBurndownReport,
  ResolutionReport,
  VersionsReport,
} from "@/api/reports";
import { useReportSource } from "@/api/reports";
import { describeProgress } from "@/api/milestones";
import { Tag } from "@/components/ui";
import { useFormat } from "@/lib/format";
import { BarList, LineChart, Scatter, StackedArea, Stat, weekLabel } from "./charts";
import { useWidgetReport, type BodyProps } from "./useWidgetReport";
import { WidgetState } from "./WidgetState";
import { t } from "@/i18n";

// The flow widgets: how work moves over time, and how far releases have got.

const categoryTone: Record<string, string> = {
  todo: "text-status-todo",
  in_progress: "text-status-progress",
  done: "text-status-done",
};

/** A day as the axis writes it, in the reader's language: 7 Sep. */
export function dayLabel(iso: string, locale: string): string {
  return new Date(iso).toLocaleDateString(locale, { day: "numeric", month: "short", timeZone: "UTC" });
}

/** The first, middle and last labels of a series, so an axis says where it starts and ends. */
export function axisEnds(labels: string[]): string[] {
  if (labels.length <= 2) return labels;
  return [labels[0]!, labels[Math.floor(labels.length / 2)]!, labels[labels.length - 1]!];
}

/** Days as a person reads them: 1.5 days, 3 days, 4 hours. */
export function days(value: number): string {
  if (value < 1) return t.dashboard.chart.hours(Math.round(value * 24));
  return t.dashboard.chart.days(value < 10 ? value.toFixed(1) : String(Math.round(value)));
}

export function CumulativeFlowBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<CumulativeFlowReport>(projectKey, widget, narrow);
  const { locale } = useFormat();
  if (!data) return <WidgetState error={error} />;
  if (data.statuses.length === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.nothingInStatus}</p>;
  const bands = data.statuses.map((s, i) => ({
    name: s.name,
    tone: categoryTone[s.category] ?? "text-ink-muted",
    values: data.days.map((d) => d.counts[i] ?? 0),
  }));
  const today = data.days[data.days.length - 1];
  return (
    <div data-cumulative-flow>
      <StackedArea bands={bands} xLabels={axisEnds(data.days.map((d) => dayLabel(d.day, locale)))} />
      <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-2xs text-ink-muted" data-flow-legend>
        {data.statuses.map((s, i) => (
          <li key={s.id} className="flex items-center gap-1" data-flow-status={s.name}>
            <span className={`inline-block size-2 rounded-sm ${categoryTone[s.category]?.replace("text-", "bg-") ?? "bg-ink-muted"}`} />
            {s.name} <span className="tabular-nums text-ink">{today?.counts[i] ?? 0}</span>
          </li>
        ))}
      </ul>
      <p className="mt-1 text-2xs text-ink-subtle">
        {t.dashboard.widgets.windowDays(data.window)}
        {data.reconstructed ? t.dashboard.widgets.rebuilt : ""}
      </p>
    </div>
  );
}

export function ControlChartBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<ControlChartReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.points.length === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.nothingResolved(data.window)}</p>;
  const at = (iso: string) => new Date(iso).getTime();
  return (
    <div data-control-chart>
      <div className="mb-2 flex gap-6">
        <Stat value={days(data.medianDays)} label={t.dashboard.widgets.median} />
        <Stat value={days(data.meanDays)} label={t.dashboard.widgets.average} />
        <Stat value={String(data.points.length)} label={t.dashboard.widgets.resolvedInDays(data.window)} />
      </div>
      <Scatter
        points={data.points.map((p) => ({ x: at(p.resolvedAt), y: p.days, label: p.key }))}
        rolling={data.points.map((p) => ({ x: at(p.resolvedAt), y: p.rolling }))}
        yLabel={days}
      />
      <p className="mt-1 text-2xs text-ink-subtle">{t.dashboard.widgets.controlNote}</p>
    </div>
  );
}

export function CreatedResolvedBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<CreatedResolvedReport>(projectKey, widget, narrow);
  const { locale } = useFormat();
  if (!data) return <WidgetState error={error} />;
  const last = data.days[data.days.length - 1];
  return (
    <div data-created-resolved>
      <div className="mb-2 flex gap-6">
        <Stat value={String(last?.createdTotal ?? 0)} label={t.dashboard.widgets.createdIn(data.window)} />
        <Stat value={String(last?.resolvedTotal ?? 0)} label={t.dashboard.widgets.resolved} />
        <Stat value={String((last?.createdTotal ?? 0) - (last?.resolvedTotal ?? 0))} label={t.dashboard.widgets.net} />
      </div>
      <LineChart
        series={[
          { name: t.dashboard.widgets.created, tone: "text-status-todo", points: data.days.map((d, i) => ({ x: i, y: d.createdTotal })) },
          { name: t.dashboard.widgets.resolved, tone: "text-status-done", points: data.days.map((d, i) => ({ x: i, y: d.resolvedTotal })) },
        ]}
        xLabels={axisEnds(data.days.map((d) => dayLabel(d.day, locale)))}
      />
    </div>
  );
}

export function AverageAgeBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<AverageAgeReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.open === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.nothingOpen}</p>;
  return (
    <div data-average-age>
      <div className="mb-2 flex gap-6">
        <Stat value={days(data.averageDays)} label={t.dashboard.widgets.averageOfOpen(data.open)} />
        <Stat value={days(data.p85Days)} label={t.dashboard.widgets.oldestSixth} />
        {data.oldestKey && <Stat value={days(data.oldestDays)} label={t.dashboard.widgets.oldest(data.oldestKey)} />}
      </div>
      <LineChart
        series={[
          { name: t.dashboard.widgets.averageAge, tone: "text-accent", points: data.weeks.map((w, i) => ({ x: i, y: Math.round(w.averageDays * 10) / 10 })) },
        ]}
        xLabels={axisEnds(data.weeks.map((w) => weekLabel(w.start)))}
        height={96}
      />
    </div>
  );
}

export function ResolutionBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<ResolutionReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.resolved === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.nothingResolved(data.window)}</p>;
  return (
    <div data-resolution-histogram>
      <BarList rows={data.bands.map((b) => ({ label: b.label, count: b.count }))} tone={() => "bg-status-done"} />
      <p className="mt-1 text-2xs text-ink-subtle">{t.dashboard.widgets.resolvedIn(data.resolved, data.window)}</p>
    </div>
  );
}

export function ReleaseBurndownBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<ReleaseBurndownReport>(projectKey, widget, narrow);
  const { locale } = useFormat();
  if (!widget.config?.versionId) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.pickVersion}</p>;
  if (!data) return <WidgetState error={error} />;
  const last = data.days[data.days.length - 1];
  return (
    <div data-release-burndown={data.versionName}>
      <div className="mb-2 flex gap-6">
        <Stat value={t.dashboard.widgets.of(last?.remaining ?? 0, data.total)} label={t.dashboard.widgets.issuesLeft} />
        {data.totalPoints > 0 && <Stat value={t.dashboard.widgets.of(last?.remainingPoints ?? 0, data.totalPoints)} label={t.dashboard.widgets.pointsLeft} />}
      </div>
      <LineChart
        series={[
          {
            name: t.dashboard.widgets.remainingOf(data.versionName),
            tone: "text-accent",
            points: data.days.map((d, i) => ({ x: i, y: d.remaining })),
            step: true,
          },
        ]}
        xLabels={axisEnds(data.days.map((d) => dayLabel(d.day, locale)))}
      />
      {data.releaseOn && <p className="mt-1 text-2xs text-ink-subtle">{t.dashboard.widgets.releaseOnSentence(dayLabel(data.releaseOn, locale))}</p>}
    </div>
  );
}

export function VersionsBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<VersionsReport>(projectKey, widget, narrow);
  const { locale } = useFormat();
  const { readOnly } = useReportSource();
  if (!data) return <WidgetState error={error} />;
  if (data.versions.length === 0) {
    return (
      <p className="text-sm text-ink-subtle">
        {t.dashboard.widgets.noUnreleased}{" "}
        {!readOnly && (
          <Link to="/projects/$projectKey/releases" params={{ projectKey }} className="text-accent hover:underline">
            {t.dashboard.widgets.planOne}
          </Link>
        )}
      </p>
    );
  }
  return (
    <ul className="space-y-3" data-versions-widget="">
      {data.versions.map((v) => (
        <li key={v.id} data-version-widget={v.name}>
          <div className="mb-1 flex items-baseline justify-between gap-2 text-sm">
            <span className="min-w-0 truncate font-medium text-ink">{v.name}</span>
            <span className="shrink-0 text-xs text-ink-muted tabular-nums">{describeProgress(v.progress)}</span>
          </div>
          <span className="block h-1.5 w-full overflow-hidden rounded-full bg-surface-raised" aria-hidden>
            <span className="block h-1.5 rounded-full bg-status-done" style={{ width: `${v.progress.percent}%` }} />
          </span>
          <p className="mt-1 flex items-center gap-2 text-2xs text-ink-subtle">
            {v.releasedAt ? (
              <Tag>{t.dashboard.widgets.released}</Tag>
            ) : v.releaseOn ? (
              t.dashboard.widgets.releaseOn(dayLabel(v.releaseOn, locale))
            ) : (
              t.dashboard.widgets.noReleaseDay
            )}
          </p>
        </li>
      ))}
    </ul>
  );
}

export function CSATBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<CSATReport>(projectKey, widget, narrow);
  if (!data) return <WidgetState error={error} />;
  if (data.invited === 0) return <p className="text-sm text-ink-subtle">{t.dashboard.widgets.noRatings(data.window)}</p>;
  return (
    <div data-csat-widget>
      <div className="mb-2 flex gap-6">
        <Stat value={data.rated ? data.average.toFixed(1) : "-"} label={t.dashboard.widgets.averageOf5} />
        <Stat value={`${Math.round(data.responseRate * 100)}%`} label={t.dashboard.widgets.answered(data.rated, data.invited)} />
      </div>
      <BarList
        rows={data.scores.map((n, i) => ({ label: `${i + 1}`, count: n })).reverse()}
        tone={(row) => (Number(row.label) >= 4 ? "bg-status-done" : Number(row.label) === 3 ? "bg-status-progress" : "bg-danger")}
      />
    </div>
  );
}
