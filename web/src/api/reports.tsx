import { createContext, useContext, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BASE, request } from "./client";
import { DASHBOARD_REFETCH_MS } from "@/config";
import type { FilterSpec } from "@/features/dashboard/filter";
import type { Progress } from "./milestones";
import type { ProjectKind } from "./projects";

/**
 * Dashboards are lists of which reports somebody wanted over a project, in
 * what order and how wide. The numbers are computed when a widget is read.
 */
export type ReportKind =
  | "status_breakdown"
  | "priority_breakdown"
  | "type_breakdown"
  | "throughput"
  | "workload"
  | "team_workload"
  | "cycle_time"
  | "epics"
  | "sprint"
  | "velocity"
  | "burndown"
  | "sprint_history"
  | "sla"
  | "request_types"
  | "filter"
  | "chart"
  | "milestones"
  | "versions"
  | "cumulative_flow"
  | "control_chart"
  | "created_vs_resolved"
  | "avg_age"
  | "resolution_histogram"
  | "release_burndown"
  | "csat";

export interface KindInfo {
  kind: ReportKind;
  title: string;
  description: string;
  width: 1 | 2;
  projectKinds?: ProjectKind[];
  /** Whether the dashboard's filter reaches this report. */
  narrows: boolean;
}

/** A filter tile's saved defaults: the tile's own values, any of which may be missing. */
export type SavedFilter = Partial<FilterSpec>;

export interface WidgetConfig {
  days?: number;
  teamId?: string;
  /** One sprint's burndown, running or over, instead of every running one. */
  sprintId?: string;
  /** One milestone for the milestones widget, instead of every open one. */
  milestoneId?: string;
  versionId?: string;
  filter?: SavedFilter;
  /** The chart widget's questions; each is one of a short list the server knows. */
  groupBy?: string;
  splitBy?: string;
  measure?: string;
  shape?: string;
  interval?: string;
  series?: string;
}

export interface ChartPart {
  label: string;
  value: number;
}

export interface ChartGroup {
  label: string;
  /** The status category behind a status label, for the learned colours. */
  category?: string;
  value: number;
  parts: ChartPart[];
}

export interface ChartReport {
  groups: ChartGroup[];
  total: number;
  groupBy: string;
  splitBy?: string;
  measure: string;
}

export interface ChartPoint {
  start: string;
  value: number;
}

export interface ChartLine {
  name: string;
  points: ChartPoint[];
}

/** One milestone as the widget shows it: the card's counts and the points behind them. */
export interface MilestoneRow {
  id: string;
  name: string;
  dueOn?: string;
  closedAt?: string;
  progress: Progress;
  points: number;
  donePoints: number;
}

export interface MilestonesReport {
  milestones: MilestoneRow[];
}

export interface VersionRow {
  id: string;
  name: string;
  releaseOn?: string;
  releasedAt?: string;
  progress: Progress;
  points: number;
  donePoints: number;
}

export interface VersionsReport {
  versions: VersionRow[];
}

/** A band per status over the window; counts align with statuses. */
export interface CumulativeFlowReport {
  statuses: Array<{ id: string; name: string; category: "todo" | "in_progress" | "done" }>;
  days: Array<{ day: string; counts: number[] }>;
  reconstructed: boolean;
  window: number;
}

export interface ControlChartReport {
  points: Array<{ key: string; summary: string; resolvedAt: string; days: number; rolling: number }>;
  meanDays: number;
  medianDays: number;
  window: number;
}

export interface CreatedResolvedReport {
  days: Array<{ day: string; created: number; resolved: number; createdTotal: number; resolvedTotal: number }>;
  window: number;
}

export interface AverageAgeReport {
  open: number;
  averageDays: number;
  p85Days: number;
  oldestKey?: string;
  oldestDays: number;
  weeks: Array<{ start: string; open: number; averageDays: number }>;
  window: number;
}

export interface ResolutionReport {
  bands: Array<{ label: string; count: number }>;
  resolved: number;
  window: number;
}

export interface CSATReport {
  invited: number;
  rated: number;
  average: number;
  /** Counts of each score, 1 to 5, in that order. */
  scores: number[];
  responseRate: number;
  window: number;
}

export interface ReleaseBurndownReport {
  versionId: string;
  versionName: string;
  releaseOn?: string;
  total: number;
  totalPoints: number;
  days: Array<{ day: string; remaining: number; remainingPoints: number }>;
}

export interface ChartSeriesReport {
  series: ChartLine[];
  interval: string;
  days: number;
  measure: string;
  groupBy?: string;
  counts: string;
}

export interface Widget {
  id: string;
  dashboardId: string;
  kind: ReportKind;
  title: string;
  width: 1 | 2;
  position: number;
  config: WidgetConfig;
}

export interface Dashboard {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  position: number;
  widgets: Widget[];
  createdAt: string;
}

export interface Bucket {
  label: string;
  count: number;
  category?: string;
}
export interface Breakdown {
  buckets: Bucket[];
  total: number;
}
export interface Week {
  start: string;
  created: number;
  resolved: number;
}
export interface ThroughputReport {
  weeks: Week[];
  created: number;
  resolved: number;
  days: number;
}
export interface Load {
  id?: string;
  name: string;
  open: number;
  inProgress: number;
  points: number;
  doneRecently: number;
}
export interface WorkloadReport {
  rows: Load[];
  days: number;
}
export interface CycleWeek {
  start: string;
  resolved: number;
  averageHours: number;
}
export interface CycleTimeReport {
  resolved: number;
  averageHours: number;
  medianHours: number;
  p90Hours: number;
  weeks: CycleWeek[];
  days: number;
}
export interface EpicProgress {
  key: string;
  summary: string;
  status: string;
  category: string;
  total: number;
  done: number;
  inProgress: number;
}
export interface EpicsReport {
  epics: EpicProgress[];
}
export interface SprintSummary {
  id: string;
  name: string;
  goal?: string;
  state?: "future" | "active" | "closed";
  startsOn?: string;
  endsOn?: string;
  capacity?: number;
  teamName?: string;
}
export interface SprintStanding {
  sprint: SprintSummary;
  committed: number;
  completed: number;
  issues: number;
  unestimated: number;
  issuesDone: number;
  daysLeft: number;
  daysTotal: number;
}
/** A sprint's standing on one day. */
export interface BurndownPoint {
  day: string;
  scope: number;
  done: number;
  remaining: number;
  issues: number;
  issuesDone: number;
  unestimated: number;
}
/** One sprint's course; `live` says the last point is today, computed now. */
export interface SprintBurndown {
  sprint: SprintSummary;
  points: BurndownPoint[];
  live: boolean;
}
export interface BurndownReport {
  active: boolean;
  sprints: SprintBurndown[];
}
/** What a finished sprint came to, in points and in issues. */
export interface SprintOutcome {
  id: string;
  name: string;
  team?: string;
  startsOn?: string;
  endsOn?: string;
  completedAt?: string;
  committed: number;
  completed: number;
  finished: number;
  carried: number;
}
export interface SprintHistoryReport {
  sprints: SprintOutcome[];
}
export interface SprintReport {
  active: boolean;
  plans: SprintStanding[];
}
export interface SprintResult {
  name: string;
  team?: string;
  committed: number;
  completed: number;
  completedAt?: string;
}
export interface VelocityReport {
  sprints: SprintResult[];
}
export interface SLAMetric {
  metric: string;
  name: string;
  met: number;
  breached: number;
  running: number;
  averageHours: number;
}
export interface SLAReport {
  metrics: SLAMetric[];
  days: number;
}

/** Which kinds the dashboard's filter reaches, so a widget it misses can say so. */
export function narrowingKinds(kinds: KindInfo[] | undefined): Set<ReportKind> {
  return new Set((kinds ?? []).filter((k) => k.narrows).map((k) => k.kind));
}

/** Where a dashboard's PDF is downloaded from, narrowed by the filter when there is one. */
export function pdfHref(dashboardId: string, query: string): string {
  return `${BASE}/dashboards/${dashboardId}/pdf${query ? `?q=${encodeURIComponent(query)}` : ""}`;
}

export const reportsQueryKey = ["reports"] as const;

export function useDashboards(projectKey: string) {
  return useQuery({
    queryKey: [...reportsQueryKey, "dashboards", projectKey],
    queryFn: () => request<{ dashboards: Dashboard[] }>(`/projects/${projectKey}/dashboards`),
    enabled: Boolean(projectKey),
  });
}

export function useReportKinds(projectKey: string) {
  return useQuery({
    queryKey: [...reportsQueryKey, "kinds", projectKey],
    queryFn: () => request<{ kinds: KindInfo[] }>(`/projects/${projectKey}/report-kinds`),
    enabled: Boolean(projectKey),
  });
}

/**
 * Where a widget's numbers come from: the project's own reports, or a shared
 * link's, which answers per widget and takes no parameters. Read-only means
 * nothing on the page leads anywhere a visitor cannot go.
 */
export interface ReportSource {
  share?: string;
  readOnly?: boolean;
}

const ReportSourceContext = createContext<ReportSource>({});

export function ReportSourceProvider({ value, children }: { value: ReportSource; children: ReactNode }) {
  return <ReportSourceContext.Provider value={value}>{children}</ReportSourceContext.Provider>;
}

export function useReportSource(): ReportSource {
  return useContext(ReportSourceContext);
}

/** One report, asked with a widget's configuration and the dashboard's query, if any; through a shared link when the page is one. */
export function useReport<T>(projectKey: string, kind: ReportKind, config: WidgetConfig, narrow?: string, widgetId?: string) {
  const source = useReportSource();
  const shared = source.share && widgetId ? { share: source.share, widgetId } : undefined;
  const search = new URLSearchParams();
  if (config.days) search.set("days", String(config.days));
  if (config.teamId) search.set("team", config.teamId);
  if (config.sprintId) search.set("sprint", config.sprintId);
  if (config.milestoneId) search.set("milestone", config.milestoneId);
  if (config.versionId) search.set("version", config.versionId);
  for (const key of ["groupBy", "splitBy", "measure", "shape", "interval", "series"] as const) {
    if (config[key]) search.set(key, config[key]);
  }
  if (narrow) search.set("q", narrow);
  const query = search.toString();
  return useQuery({
    queryKey: shared ? [...reportsQueryKey, "shared", shared.share, shared.widgetId] : [...reportsQueryKey, "report", projectKey, kind, query],
    queryFn: () => request<T>(shared ? `/shared/${shared.share}/widgets/${shared.widgetId}` : `/projects/${projectKey}/reports/${kind}${query ? `?${query}` : ""}`),
    enabled: Boolean(projectKey),
    // A dashboard is left open; its numbers should follow the work.
    refetchInterval: DASHBOARD_REFETCH_MS,
  });
}

function useReportMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: run, onSuccess: () => queryClient.invalidateQueries({ queryKey: reportsQueryKey }) });
}

/** What a dashboard may start from: a built-in (a word) or one of the organization's own (an id). */
export interface DashboardTemplate {
  id?: string;
  key: string;
  name: string;
  description: string;
  projectKinds?: ProjectKind[];
  widgets: Array<{ kind: ReportKind; title?: string; width?: 1 | 2; config: WidgetConfig }>;
  builtIn: boolean;
}

export function useDashboardTemplates(projectKey: string) {
  return useQuery({
    queryKey: [...reportsQueryKey, "templates", projectKey],
    queryFn: () => request<{ templates: DashboardTemplate[] }>(`/dashboard-templates?project=${encodeURIComponent(projectKey)}`),
    enabled: Boolean(projectKey),
  });
}

export function useCreateDashboard(projectKey: string) {
  return useReportMutation(({ name, template, milestoneId }: { name: string; template?: string; milestoneId?: string }) =>
    request<{ dashboard: Dashboard }>(`/projects/${projectKey}/dashboards`, { method: "POST", body: { name, template, milestoneId } }),
  );
}

export function useSaveTemplate() {
  return useReportMutation(({ dashboardId, name, description }: { dashboardId: string; name: string; description: string }) =>
    request<{ template: DashboardTemplate }>(`/dashboards/${dashboardId}/template`, { method: "POST", body: { name, description } }),
  );
}

export function useDeleteTemplate() {
  return useReportMutation((id: string) => request(`/dashboard-templates/${id}`, { method: "DELETE" }));
}

export function useDeleteDashboard() {
  return useReportMutation((id: string) => request<void>(`/dashboards/${id}`, { method: "DELETE" }));
}

export function useAddWidget() {
  return useReportMutation(({ dashboardId, ...input }: { dashboardId: string; kind: ReportKind; title?: string; width?: 1 | 2; config?: WidgetConfig }) =>
    request<{ widget: Widget }>(`/dashboards/${dashboardId}/widgets`, { method: "POST", body: input }),
  );
}

export function useUpdateWidget() {
  return useReportMutation(({ id, ...input }: { id: string; title?: string; width?: 1 | 2; config?: WidgetConfig }) =>
    request<{ widget: Widget }>(`/widgets/${id}`, { method: "PATCH", body: input }),
  );
}

export function useRemoveWidget() {
  return useReportMutation((id: string) => request<void>(`/widgets/${id}`, { method: "DELETE" }));
}

export function useReorderWidgets() {
  return useReportMutation(({ dashboardId, order }: { dashboardId: string; order: string[] }) =>
    request<void>(`/dashboards/${dashboardId}/widgets`, { method: "PUT", body: { order } }),
  );
}

/** Hours as a person says them: 3.5h below a day, 2.1d above. */
export function hours(value: number): string {
  if (value >= 48) return `${(value / 24).toFixed(1)}d`;
  if (value >= 1) return `${value.toFixed(1)}h`;
  return `${Math.round(value * 60)}m`;
}

/** The share of a whole, as a percentage between 0 and 100. */
export function share(part: number, whole: number): number {
  if (whole <= 0) return 0;
  return Math.round((part / whole) * 100);
}
