import { useQuery } from "@tanstack/react-query";
import { request } from "./client";

/** Everything dated in one month of a project, as ranges of days. */
export type CalendarKind = "issue" | "sprint" | "milestone" | "version";

export interface CalendarItem {
  kind: CalendarKind;
  id: string;
  key?: string;
  title: string;
  /** YYYY-MM-DD; a thing with one date has both the same. */
  from: string;
  to: string;
  category?: string;
  done?: boolean;
}

export interface CalendarMonth {
  year: number;
  month: number;
  items: CalendarItem[];
  truncated: boolean;
}

export const calendarQueryKey = ["calendar"] as const;

export function useCalendarMonth(projectKey: string, year: number, month: number) {
  const ym = `${year}-${String(month).padStart(2, "0")}`;
  return useQuery({
    queryKey: [...calendarQueryKey, projectKey, ym],
    queryFn: () => request<{ month: CalendarMonth }>(`/projects/${projectKey}/calendar?month=${ym}`),
    enabled: Boolean(projectKey),
  });
}
