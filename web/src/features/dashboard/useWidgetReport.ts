import { useReport, type ReportKind, type Widget } from "@/api/reports";

/** What a body is drawn from: the widget, and the dashboard's query when the kind is narrowed by it. */
export interface BodyProps {
  projectKey: string;
  widget: Widget;
  narrow?: string;
}

/** One widget's report, asked the same way by every body: its kind, its settings, its id. */
export function useWidgetReport<T>(projectKey: string, widget: Widget, narrow?: string) {
  return useReport<T>(projectKey, widget.kind as ReportKind, widget.config ?? {}, narrow, widget.id);
}
