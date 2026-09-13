import { useState } from "react";
import { useMilestones } from "@/api/milestones";
import { useVersions } from "@/api/versions";
import type { Widget, WidgetConfig } from "@/api/reports";
import { IconButton, Popover, Segmented, Select, SelectInput } from "@/components/ui";
import { Icon } from "@/components/icons";
import { CHART_DEFAULT_SINCE_DAYS, DASHBOARD_DEFAULT_WINDOW_DAYS, DASHBOARD_FILTER_WINDOWS, DASHBOARD_WINDOWS } from "@/config";
import { GROUP_FIELDS, fieldLabel, type ChartShape, type GroupField } from "./chart";

/** The kinds whose only setting is how far back they look. */
const windowed = new Set(["throughput", "workload", "team_workload", "cycle_time", "sla"]);

/**
 * What a widget can be told while arranging. Most kinds have nothing to say;
 * the windowed ones pick a window; a chart is asked what to draw and how.
 */
export function WidgetSettings({ projectKey, widget, onChange }: { projectKey: string; widget: Widget; onChange: (config: WidgetConfig) => void }) {
  if (windowed.has(widget.kind)) {
    return (
      <SelectInput aria-label={`Window for ${widget.title}`} controlSize="sm" value={widget.config?.days ?? DASHBOARD_DEFAULT_WINDOW_DAYS} onChange={(e) => onChange({ ...widget.config, days: Number(e.target.value) })}>
        {DASHBOARD_WINDOWS.map((d) => (
          <option key={d} value={d}>
            {d} days
          </option>
        ))}
      </SelectInput>
    );
  }
  if (widget.kind === "chart") return <ChartSettings widget={widget} onChange={onChange} />;
  if (widget.kind === "milestones") return <MilestoneSettings projectKey={projectKey} widget={widget} onChange={onChange} />;
  if (widget.kind === "versions" || widget.kind === "release_burndown") return <VersionSettings projectKey={projectKey} widget={widget} onChange={onChange} />;
  return null;
}

/** Every unreleased version, or one of them; a burndown always needs one. */
function VersionSettings({ projectKey, widget, onChange }: { projectKey: string; widget: Widget; onChange: (config: WidgetConfig) => void }) {
  const { data } = useVersions(projectKey, true);
  const burndown = widget.kind === "release_burndown";
  return (
    <SelectInput aria-label={`Version for ${widget.title}`} controlSize="sm" value={widget.config?.versionId ?? ""} onChange={(e) => onChange({ ...widget.config, versionId: e.target.value || undefined })} data-version-setting>
      <option value="">{burndown ? "Choose a version" : "All unreleased versions"}</option>
      {(data?.versions ?? []).map((v) => (
        <option key={v.id} value={v.id}>
          {v.name}
          {v.releasedAt ? " (released)" : ""}
        </option>
      ))}
    </SelectInput>
  );
}

/** Every open milestone, or one of them, closed ones included. */
function MilestoneSettings({ projectKey, widget, onChange }: { projectKey: string; widget: Widget; onChange: (config: WidgetConfig) => void }) {
  const { data } = useMilestones(projectKey, true);
  return (
    <SelectInput aria-label={`Milestone for ${widget.title}`} controlSize="sm" value={widget.config?.milestoneId ?? ""} onChange={(e) => onChange({ ...widget.config, milestoneId: e.target.value || undefined })}>
      <option value="">All open milestones</option>
      {(data?.milestones ?? []).map((milestone) => (
        <option key={milestone.id} value={milestone.id}>
          {milestone.name}
          {milestone.closedAt ? " (closed)" : ""}
        </option>
      ))}
    </SelectInput>
  );
}

function ChartSettings({ widget, onChange }: { widget: Widget; onChange: (config: WidgetConfig) => void }) {
  const [open, setOpen] = useState(false);
  const config = widget.config ?? {};
  const shape = (config.shape as ChartShape) ?? "bar";
  const set = (patch: WidgetConfig) => onChange({ ...config, ...patch });
  return (
    <Popover
      open={open}
      onClose={() => setOpen(false)}
      label="Chart settings"
      align="end"
      className="w-80"
      trigger={<IconButton icon={<Icon.Settings />} label={`Settings for ${widget.title}`} size="sm" aria-expanded={open} onClick={() => setOpen((o) => !o)} data-action="chart-settings" />}
    >
      <div className="space-y-3" data-testid="chart-settings">
        <Segmented<ChartShape>
          label="Shape"
          size="sm"
          value={shape}
          onChange={(next) => set({ shape: next, splitBy: next === "stacked" ? (config.splitBy ?? "statusCategory") : undefined })}
          options={[
            { value: "bar", label: "Bars" },
            { value: "stacked", label: "Stack" },
            { value: "donut", label: "Donut" },
            { value: "line", label: "Line" },
          ]}
        />
        {shape === "line" ? (
          <>
            <Select label="Series" value={config.series ?? "created"} onChange={(e) => set({ series: e.target.value })}>
              <option value="created">Created</option>
              <option value="resolved">Resolved</option>
              <option value="open">Open</option>
            </Select>
            <Select label="Group by" value={config.groupBy ?? ""} onChange={(e) => set({ groupBy: e.target.value || undefined })}>
              <option value="">One line</option>
              {GROUP_FIELDS.map((field) => (
                <option key={field} value={field}>
                  {fieldLabel[field]}
                </option>
              ))}
            </Select>
            <div className="grid grid-cols-2 gap-3">
              <Select label="Interval" value={config.interval ?? "week"} onChange={(e) => set({ interval: e.target.value })}>
                <option value="week">Weeks</option>
                <option value="month">Months</option>
              </Select>
              <Select label="Since" value={String(config.days ?? CHART_DEFAULT_SINCE_DAYS)} onChange={(e) => set({ days: Number(e.target.value) })}>
                {DASHBOARD_FILTER_WINDOWS.map((days) => (
                  <option key={days} value={days}>
                    Last {days} days
                  </option>
                ))}
              </Select>
            </div>
          </>
        ) : (
          <>
            <Select label="Group by" value={config.groupBy ?? "status"} onChange={(e) => set({ groupBy: e.target.value as GroupField })}>
              {GROUP_FIELDS.map((field) => (
                <option key={field} value={field}>
                  {fieldLabel[field]}
                </option>
              ))}
            </Select>
            {shape === "stacked" && (
              <Select label="Split by" value={config.splitBy ?? "statusCategory"} onChange={(e) => set({ splitBy: e.target.value as GroupField })}>
                {GROUP_FIELDS.filter((field) => field !== (config.groupBy ?? "status")).map((field) => (
                  <option key={field} value={field}>
                    {fieldLabel[field]}
                  </option>
                ))}
              </Select>
            )}
          </>
        )}
        <Segmented<"count" | "points">
          label="Measure"
          size="sm"
          value={(config.measure as "count" | "points") ?? "count"}
          onChange={(measure) => set({ measure })}
          options={[
            { value: "count", label: "Issues" },
            { value: "points", label: "Points" },
          ]}
        />
        {shape === "donut" && <p className="text-xs text-ink-subtle">A donut is for a glance at shares. To compare close values, use bars.</p>}
      </div>
    </Popover>
  );
}
