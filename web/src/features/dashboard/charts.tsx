import { cx } from "@/components/ui";
import { CHART_AXIS_LABELS, CHART_VALUE_LABEL_MAX_COLUMNS } from "@/config";
import { arcPath, figure, round, type Arc, type Column } from "./chart";

/**
 * The few shapes a dashboard needs, drawn by hand. A chart library would be a
 * dependency for six rectangles; these are rectangles.
 */

/** Rows of label, bar and count, the bar sized against the largest. */
export function BarList({
  rows,
  tone = () => "bg-accent",
  empty = "Nothing to count",
}: {
  rows: Array<{ label: string; count: number; key?: string }>;
  tone?: (row: { label: string; count: number }) => string;
  empty?: string;
}) {
  const max = Math.max(1, ...rows.map((r) => r.count));
  if (rows.length === 0) return <p className="text-sm text-ink-subtle">{empty}</p>;
  return (
    <ul className="space-y-1.5" data-testid="bar-list">
      {rows.map((row) => (
        <li key={row.key ?? row.label} className="grid grid-cols-[minmax(0,9rem)_1fr_2.5rem] items-center gap-2 text-sm" data-bar={row.label}>
          <span className="truncate text-ink" title={row.label}>
            {row.label}
          </span>
          <span className="h-2 rounded-sm bg-surface-raised">
            <span className={cx("block h-2 rounded-sm", tone(row))} style={{ width: `${(row.count / max) * 100}%` }} />
          </span>
          <span className="text-right text-ink-muted tabular-nums">{row.count}</span>
        </li>
      ))}
    </ul>
  );
}

/** A fraction of a whole, as a bar with the numbers beside it. */
export function Progress({ done, total, label }: { done: number; total: number; label?: string }) {
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <div className="flex items-center gap-2 text-xs">
      <span className="h-2 flex-1 rounded-sm bg-surface-raised">
        <span className="block h-2 rounded-sm bg-status-done" style={{ width: `${pct}%` }} />
      </span>
      <span className="w-16 text-right text-ink-muted tabular-nums">{label ?? `${done}/${total}`}</span>
    </div>
  );
}

/** Pairs of bars per period, for created against resolved and the like. */
export function PairedBars({
  groups,
  a,
  b,
}: {
  groups: Array<{ label: string; a: number; b: number }>;
  a: { name: string; tone: string };
  b: { name: string; tone: string };
}) {
  const max = Math.max(1, ...groups.flatMap((g) => [g.a, g.b]));
  return (
    <div>
      {/* Each group is h-full so the bars' percentage heights resolve against
          the chart rather than against a wrapper as tall as its own bars. */}
      <div className="flex h-28 items-end gap-1.5" role="img" aria-label={`${a.name} against ${b.name}`}>
        {groups.map((g) => (
          <div key={g.label} className="flex h-full min-w-0 flex-1 items-end justify-center gap-px" title={`${g.label}: ${g.a} ${a.name}, ${g.b} ${b.name}`}>
            <span className={cx("w-[45%] rounded-t-sm", a.tone)} style={{ height: `${(g.a / max) * 100}%`, minHeight: g.a ? 2 : 0 }} />
            <span className={cx("w-[45%] rounded-t-sm", b.tone)} style={{ height: `${(g.b / max) * 100}%`, minHeight: g.b ? 2 : 0 }} />
          </div>
        ))}
      </div>
      <div className="mt-1 flex gap-1.5">
        {groups.map((g, i) => (
          <span key={g.label} className="min-w-0 flex-1 truncate text-center text-2xs text-ink-subtle">
            {i % Math.ceil(groups.length / CHART_AXIS_LABELS) === 0 ? g.label : ""}
          </span>
        ))}
      </div>
      <div className="mt-2 flex gap-4 text-2xs text-ink-muted">
        <span className="flex items-center gap-1">
          <span className={cx("size-2 rounded-sm", a.tone)} /> {a.name}
        </span>
        <span className="flex items-center gap-1">
          <span className={cx("size-2 rounded-sm", b.tone)} /> {b.name}
        </span>
      </div>
    </div>
  );
}

/** A large number with a word under it. */
export function Stat({ value, label }: { value: string; label: string }) {
  return (
    <div>
      <div className="text-xl font-semibold tracking-tight text-ink tabular-nums">{value}</div>
      <div className="text-2xs text-ink-muted">{label}</div>
    </div>
  );
}

/** A week's start as a short date. */
export function weekLabel(iso: string): string {
  const d = new Date(iso);
  return `${d.getDate()}/${d.getMonth() + 1}`;
}

/** One line on a chart. Stepped series hold each value until the next sample. */
export interface Series {
  name: string;
  /** A stroke colour class, such as text-accent; the line is drawn in currentColor. */
  tone: string;
  points: Array<{ x: number; y: number }>;
  dashed?: boolean;
  step?: boolean;
}

/** The unit box every series is drawn into; the SVG stretches it to fit. */
const CHART_UNIT = 100;

/**
 * Lines over a shared x axis, scaled against the tallest point of any series.
 * Drawn into a unit box that stretches to the widget, with strokes that do not
 * stretch with it, which is what lets a burndown stay legible at any width.
 */
export function LineChart({
  series,
  xLabels = [],
  height = 128,
  empty = "Nothing to draw yet",
}: {
  series: Series[];
  xLabels?: string[];
  height?: number;
  empty?: string;
}) {
  const drawn = series.filter((s) => s.points.length > 0);
  if (drawn.length === 0) return <p className="text-sm text-ink-subtle">{empty}</p>;
  const maxX = Math.max(1, ...drawn.flatMap((s) => s.points.map((p) => p.x)));
  const maxY = Math.max(1, ...drawn.flatMap((s) => s.points.map((p) => p.y)));
  const px = (x: number) => (x / maxX) * CHART_UNIT;
  const py = (y: number) => CHART_UNIT - (y / maxY) * CHART_UNIT;

  return (
    <div data-testid="line-chart">
      <svg
        viewBox={`0 0 ${CHART_UNIT} ${CHART_UNIT}`}
        preserveAspectRatio="none"
        className="block w-full overflow-visible"
        style={{ height }}
        role="img"
        aria-label={drawn.map((s) => s.name).join(", ")}
      >
        <line x1={0} y1={CHART_UNIT} x2={CHART_UNIT} y2={CHART_UNIT} className="stroke-border" strokeWidth={1} vectorEffect="non-scaling-stroke" />
        {drawn.map((s) => (
          <g key={s.name} data-series={s.name} className={s.tone}>
            <path
              d={pathOf(s.points.map((p) => ({ x: px(p.x), y: py(p.y) })), Boolean(s.step))}
              fill="none"
              stroke="currentColor"
              strokeWidth={s.dashed ? 1 : 2}
              strokeDasharray={s.dashed ? "4 4" : undefined}
              strokeLinejoin="round"
              vectorEffect="non-scaling-stroke"
            />
          </g>
        ))}
      </svg>
      {xLabels.length > 0 && (
        <div className="mt-1 flex justify-between text-2xs text-ink-subtle">
          {xLabels.map((label, i) => (
            <span key={`${label}-${i}`}>{label}</span>
          ))}
        </div>
      )}
      <div className="mt-2 flex flex-wrap gap-4 text-2xs text-ink-muted">
        {drawn.map((s) => (
          <span key={s.name} className="flex items-center gap-1">
            <span className={cx("inline-block h-0.5 w-3", s.tone)} style={{ borderTop: s.dashed ? "1px dashed currentColor" : "2px solid currentColor" }} />
            {s.name}
          </span>
        ))}
      </div>
    </div>
  );
}

/** The path a series draws: a polyline, or steps that hold each value until the next. */
export function pathOf(points: Array<{ x: number; y: number }>, step: boolean): string {
  return points
    .map((p, i) => {
      if (i === 0) return `M${round(p.x)} ${round(p.y)}`;
      if (step) return `H${round(p.x)} V${round(p.y)}`;
      return `L${round(p.x)} ${round(p.y)}`;
    })
    .join(" ");
}

/**
 * Columns, plain or stacked, drawn as HTML: a rectangle needs no SVG, and a
 * rounded cap and a surface gap between segments come for free.
 */
export function Columns({
  columns,
  tones,
  segmentTones,
  height = 128,
  empty = "Nothing to draw yet",
}: {
  columns: Column[];
  /** A tone class per column label, for plain bars. */
  tones: Map<string, string>;
  /** A tone class per segment label; when given the columns are stacks. */
  segmentTones?: Map<string, string>;
  height?: number;
  empty?: string;
}) {
  if (columns.length === 0 || columns.every((c) => c.value === 0)) return <p className="text-sm text-ink-subtle">{empty}</p>;
  const labelled = columns.length <= CHART_VALUE_LABEL_MAX_COLUMNS;
  return (
    <div data-chart={segmentTones ? "stacked" : "bar"}>
      <div className="flex items-end gap-3 border-b border-border" style={{ height }}>
        {columns.map((column) => (
          <div key={column.label} className="flex h-full min-w-0 flex-1 flex-col justify-end" data-chart-group={column.label} data-value={figure(column.value)}>
            {labelled && column.value > 0 && <span className="mb-1 truncate text-center text-2xs text-ink-muted tabular-nums">{figure(column.value)}</span>}
            {segmentTones ? (
              <div className="mx-auto flex w-full max-w-6 flex-col-reverse gap-px" style={{ height: `${column.height * 100}%` }}>
                {column.segments
                  .filter((segment) => segment.value > 0)
                  .map((segment, index, all) => (
                    <div
                      key={segment.label}
                      className={cx("w-full bg-current", segmentTones.get(segment.label), index === all.length - 1 && "rounded-t")}
                      style={{ flexGrow: segment.value }}
                      data-chart-part={segment.label}
                      title={`${column.label}: ${segment.label} ${figure(segment.value)}`}
                    />
                  ))}
              </div>
            ) : (
              <div
                className={cx("mx-auto w-full max-w-6 rounded-t bg-current", tones.get(column.label))}
                style={{ height: `${column.height * 100}%` }}
                title={`${column.label}: ${figure(column.value)}`}
              />
            )}
          </div>
        ))}
      </div>
      <div className="mt-1 flex gap-3">
        {columns.map((column) => (
          <span key={column.label} className="min-w-0 flex-1 truncate text-center text-2xs text-ink-subtle" title={column.label}>
            {column.label}
          </span>
        ))}
      </div>
    </div>
  );
}

/** The unit square a donut is drawn into. */
const DONUT_UNIT = 100;
const DONUT_RADIUS = 46;
const DONUT_HOLE = 30;

/**
 * Shares of a whole as a ring, with the total in the hole. Slices are cut by
 * a hairline of surface, and a slice too thin to read is still in the legend.
 */
export function Donut({ arcs, tones, total, empty = "Nothing to draw yet" }: { arcs: Arc[]; tones: Map<string, string>; total: string; empty?: string }) {
  if (arcs.length === 0) return <p className="text-sm text-ink-subtle">{empty}</p>;
  return (
    <svg viewBox={`0 0 ${DONUT_UNIT} ${DONUT_UNIT}`} className="mx-auto block h-40 w-40" role="img" aria-label={arcs.map((a) => `${a.label} ${Math.round(a.share * 100)}%`).join(", ")} data-chart="donut">
      {arcs.map((arc) => (
        <path
          key={arc.label}
          d={arcPath(DONUT_UNIT / 2, DONUT_UNIT / 2, DONUT_RADIUS, DONUT_HOLE, arc.start, arc.end)}
          fill="currentColor"
          className={cx("stroke-surface", tones.get(arc.label))}
          strokeWidth={1.5}
          data-chart-group={arc.label}
          data-value={figure(arc.value)}
        >
          <title>{`${arc.label}: ${figure(arc.value)} (${Math.round(arc.share * 100)}%)`}</title>
        </path>
      ))}
      <text x={DONUT_UNIT / 2} y={DONUT_UNIT / 2} textAnchor="middle" dominantBaseline="middle" className="fill-ink text-xl font-semibold">
        {total}
      </text>
    </svg>
  );
}

/** Who is which colour, with the figure beside each name; identity is never colour alone. */
export function Legend({ items, tones }: { items: Array<{ label: string; value: number; share?: number }>; tones: Map<string, string> }) {
  return (
    <ul className="mt-3 grid gap-x-4 gap-y-1 text-xs text-ink-muted sm:grid-cols-2" data-chart-legend="">
      {items.map((item) => (
        <li key={item.label} className="flex min-w-0 items-center gap-2">
          <span className={cx("inline-block size-2.5 shrink-0 rounded-sm bg-current", tones.get(item.label))} aria-hidden="true" />
          <span className="min-w-0 flex-1 truncate text-ink">{item.label}</span>
          <span className="tabular-nums">
            {figure(item.value)}
            {item.share !== undefined && <span className="ml-1 text-ink-subtle">{Math.round(item.share * 100)}%</span>}
          </span>
        </li>
      ))}
    </ul>
  );
}

/** One band of a stacked area: its name, tone and a value per x step. */
export interface Band {
  name: string;
  tone: string;
  values: number[];
}

/**
 * Bands stacked on each other over time: each band's top is the sum of it and
 * the bands below, so the whole reads as how much work stood where, day by
 * day. Drawn as filled paths in the band's tone, oldest band at the bottom.
 */
export function stackedPaths(bands: Band[]): Array<{ name: string; tone: string; d: string }> {
  const steps = Math.max(0, ...bands.map((b) => b.values.length));
  if (steps < 2) return [];
  const totals = Array.from({ length: steps }, (_, i) => bands.reduce((sum, b) => sum + (b.values[i] ?? 0), 0));
  const maxY = Math.max(1, ...totals);
  const px = (i: number) => (i / (steps - 1)) * CHART_UNIT;
  const py = (y: number) => CHART_UNIT - (y / maxY) * CHART_UNIT;
  const below = new Array<number>(steps).fill(0);
  return bands.map((b) => {
    const top = below.map((base, i) => base + (b.values[i] ?? 0));
    const upper = top.map((y, i) => `${round(px(i))} ${round(py(y))}`);
    const lower = below.map((y, i) => `${round(px(i))} ${round(py(y))}`).reverse();
    const d = `M${upper[0]} ${upper.slice(1).map((p) => `L${p}`).join(" ")} ${lower.map((p) => `L${p}`).join(" ")} Z`;
    for (let i = 0; i < steps; i++) below[i] = top[i]!;
    return { name: b.name, tone: b.tone, d };
  });
}

export function StackedArea({ bands, xLabels = [], height = 160, empty = "Nothing to draw yet" }: { bands: Band[]; xLabels?: string[]; height?: number; empty?: string }) {
  const paths = stackedPaths(bands);
  if (paths.length === 0) return <p className="text-sm text-ink-subtle">{empty}</p>;
  return (
    <div data-testid="stacked-area">
      <svg viewBox={`0 0 ${CHART_UNIT} ${CHART_UNIT}`} preserveAspectRatio="none" className="block w-full" style={{ height }} role="img" aria-label={bands.map((b) => b.name).join(", ")}>
        {paths.map((p) => (
          <path key={p.name} d={p.d} className={cx(p.tone, "opacity-80")} fill="currentColor" stroke="var(--color-surface)" strokeWidth={0.5} vectorEffect="non-scaling-stroke" data-flow-band={p.name} />
        ))}
      </svg>
      {xLabels.length > 0 && (
        <div className="mt-1 flex justify-between text-2xs text-ink-subtle">
          {xLabels.map((label, i) => (
            <span key={`${label}-${i}`}>{label}</span>
          ))}
        </div>
      )}
    </div>
  );
}

/** A dot per point with a line through the rolling values; x is time, y is the measure. */
export function Scatter({
  points,
  rolling,
  height = 160,
  empty = "Nothing to draw yet",
  yLabel,
}: {
  points: Array<{ x: number; y: number; label: string }>;
  rolling: Array<{ x: number; y: number }>;
  height?: number;
  empty?: string;
  yLabel?: (y: number) => string;
}) {
  if (points.length === 0) return <p className="text-sm text-ink-subtle">{empty}</p>;
  const minX = Math.min(...points.map((p) => p.x));
  const maxX = Math.max(minX + 1, ...points.map((p) => p.x));
  const maxY = Math.max(1, ...points.map((p) => p.y), ...rolling.map((p) => p.y));
  const px = (x: number) => ((x - minX) / (maxX - minX)) * CHART_UNIT;
  const py = (y: number) => CHART_UNIT - (y / maxY) * CHART_UNIT;
  return (
    <div data-testid="scatter">
      <svg viewBox={`0 0 ${CHART_UNIT} ${CHART_UNIT}`} preserveAspectRatio="none" className="block w-full overflow-visible" style={{ height }} role="img" aria-label={`${points.length} points`}>
        <line x1={0} y1={CHART_UNIT} x2={CHART_UNIT} y2={CHART_UNIT} className="stroke-border" strokeWidth={1} vectorEffect="non-scaling-stroke" />
        {rolling.length > 1 && (
          <path d={pathOf(rolling.map((p) => ({ x: px(p.x), y: py(p.y) })), false)} fill="none" stroke="currentColor" className="text-accent" strokeWidth={2} vectorEffect="non-scaling-stroke" strokeLinejoin="round" data-rolling-line />
        )}
        {points.map((p, i) => (
          <circle key={`${p.label}-${i}`} cx={px(p.x)} cy={py(p.y)} r={1.6} className="fill-ink-muted" vectorEffect="non-scaling-stroke" data-chart-point={p.label}>
            <title>{`${p.label}: ${yLabel ? yLabel(p.y) : p.y}`}</title>
          </circle>
        ))}
      </svg>
    </div>
  );
}
