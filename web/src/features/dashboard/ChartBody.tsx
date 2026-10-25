import type { ChartReport, ChartSeriesReport } from "@/api/reports";
import { Table, Td, Th } from "@/components/ui";
import { OTHER, columnLayout, describeChart, donutLayout, fieldLabel, figure, foldTail, segmentNames, seriesFrom, tonesFor, type GroupField } from "./chart";
import { Columns, Donut, Legend, LineChart, weekLabel } from "./charts";
import { useWidgetReport, type BodyProps } from "./useWidgetReport";
import { Loading, WidgetState } from "./WidgetState";
import { t } from "@/i18n";

/** One chart widget: groups as bars, a stack or a donut, or buckets as lines. */
export function ChartBody({ projectKey, widget, narrow }: BodyProps) {
  if (widget.config?.shape === "line") return <LinesBody projectKey={projectKey} widget={widget} narrow={narrow} />;
  return <GroupsBody projectKey={projectKey} widget={widget} narrow={narrow} />;
}

function GroupsBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<ChartReport>(projectKey, widget, narrow);
  if (error) return <WidgetState error={error} />;
  if (!data) return <Loading />;
  const groups = foldTail(data.groups);
  const tones = tonesFor(groups);
  const shape = widget.config?.shape ?? "bar";
  const unit = data.measure === "points" ? t.dashboard.chart.unitPoints : t.dashboard.chart.unitIssues;
  const shown = (label: string) => (label === OTHER ? t.dashboard.chart.other : label);
  const legend = groups.map((g) => ({ label: g.label, value: g.value, share: data.total > 0 ? g.value / data.total : 0 }));
  const stacked = shape === "stacked" && Boolean(data.splitBy);
  const partTones = stacked ? tonesFor(segmentNames(groups).map((label) => ({ label, value: 0, parts: [] }))) : undefined;

  return (
    <div>
      <p className="mb-3 text-xs text-ink-subtle">{describeChart(widget.config ?? {})}</p>
      {shape === "donut" ? (
        <Donut arcs={donutLayout(groups)} tones={tones} total={figure(data.total)} />
      ) : (
        <Columns columns={columnLayout(groups)} tones={tones} segmentTones={partTones} />
      )}
      {stacked && partTones ? (
        <Legend
          items={segmentNames(groups).map((label) => ({
            label,
            value: groups.reduce((sum, g) => sum + (g.parts.find((p) => p.label === label)?.value ?? 0), 0),
          }))}
          tones={partTones}
        />
      ) : (
        groups.length >= 2 && <Legend items={legend} tones={tones} />
      )}
      <p className="mt-2 text-xs text-ink-subtle tabular-nums">{t.dashboard.chart.inAll(figure(data.total), unit)}</p>
      <details className="mt-2 text-xs">
        <summary className="cursor-pointer text-ink-muted">{t.dashboard.chart.asTable}</summary>
        <div className="mt-2">
          <Table dense>
            <thead>
              <tr>
                <Th>{fieldLabel[(data.groupBy as GroupField) ?? "status"] ?? data.groupBy}</Th>
                {stacked && <Th>{fieldLabel[data.splitBy as GroupField] ?? data.splitBy}</Th>}
                <Th className="text-right">{unit}</Th>
              </tr>
            </thead>
            <tbody>
              {groups.flatMap((g) =>
                stacked && g.parts.length > 0
                  ? g.parts.map((part) => (
                      <tr key={`${g.label}-${part.label}`}>
                        <Td>{shown(g.label)}</Td>
                        <Td>{shown(part.label)}</Td>
                        <Td className="text-right tabular-nums">{figure(part.value)}</Td>
                      </tr>
                    ))
                  : [
                      <tr key={g.label}>
                        <Td>{shown(g.label)}</Td>
                        {stacked && <Td />}
                        <Td className="text-right tabular-nums">{figure(g.value)}</Td>
                      </tr>,
                    ],
              )}
            </tbody>
          </Table>
        </div>
      </details>
    </div>
  );
}

function LinesBody({ projectKey, widget, narrow }: BodyProps) {
  const { data, error } = useWidgetReport<ChartSeriesReport>(projectKey, widget, narrow);
  if (error) return <WidgetState error={error} />;
  if (!data) return <Loading />;
  const series = seriesFrom(data);
  const starts = data.series[0]?.points.map((p) => p.start) ?? [];
  const labels = starts.length > 0 ? [weekLabel(starts[0]!), weekLabel(starts[starts.length - 1]!)] : [];
  return (
    <div>
      <p className="mb-3 text-xs text-ink-subtle">{describeChart(widget.config ?? {})}</p>
      <LineChart series={series} xLabels={labels} />
      <details className="mt-2 text-xs">
        <summary className="cursor-pointer text-ink-muted">{t.dashboard.chart.asTable}</summary>
        <div className="mt-2">
          <Table dense>
            <thead>
              <tr>
                <Th>{data.interval === "month" ? t.dashboard.chart.month : t.dashboard.chart.week}</Th>
                {data.series.map((line) => (
                  <Th key={line.name} className="text-right">
                    {line.name}
                  </Th>
                ))}
              </tr>
            </thead>
            <tbody>
              {starts.map((start, index) => (
                <tr key={start}>
                  <Td>{weekLabel(start)}</Td>
                  {data.series.map((line) => (
                    <Td key={line.name} className="text-right tabular-nums">
                      {figure(line.points[index]?.value ?? 0)}
                    </Td>
                  ))}
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      </details>
    </div>
  );
}
