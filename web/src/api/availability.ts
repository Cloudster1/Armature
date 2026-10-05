import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request, upload } from "./client";
import { META_STALE_MS } from "@/config";

/** The keys of a working week, Monday first as a week is written. */
export const WEEKDAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"] as const;
export type Weekday = (typeof WEEKDAYS)[number];

/** One day off on a calendar, written YYYY-MM-DD. */
export interface Holiday {
  day: string;
  name: string;
  halfDay: boolean;
}

/** A named set of days off; the default is everybody's until given another. */
export interface HolidayCalendar {
  id: string;
  name: string;
  default: boolean;
  dayCount: number;
  /** Only when the calendar is read on its own. */
  days?: Holiday[];
  peopleCount: number;
  createdAt: string;
  updatedAt: string;
}

/** How long a person works on each weekday, and whose days off they keep. */
export interface WorkingWeek {
  userId: string;
  /** Null follows the organization's default calendar. */
  calendarId: string | null;
  calendar: { id: string; name: string } | null;
  minutes: Record<string, number>;
  /** False while nobody has set a week for them and they work the standard one. */
  saved: boolean;
}

export const holidayCalendarsQueryKey = ["holiday-calendars"] as const;
const workingWeekQueryKey = ["working-week"] as const;

export function useHolidayCalendars() {
  return useQuery({
    queryKey: holidayCalendarsQueryKey,
    queryFn: () => request<{ calendars: HolidayCalendar[] }>("/holiday-calendars"),
    staleTime: META_STALE_MS,
  });
}

export function useHolidayCalendar(id: string | null) {
  return useQuery({
    queryKey: [...holidayCalendarsQueryKey, id],
    queryFn: () => request<{ calendar: HolidayCalendar }>(`/holiday-calendars/${id}`),
    enabled: Boolean(id),
  });
}

/** A change to one calendar shows in the list and in everybody's week. */
function useCalendarMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: holidayCalendarsQueryKey });
      void queryClient.invalidateQueries({ queryKey: workingWeekQueryKey });
    },
  });
}

export function useCreateHolidayCalendar() {
  return useCalendarMutation((input: { name: string; default?: boolean }) =>
    request<{ calendar: HolidayCalendar }>("/holiday-calendars", { method: "POST", body: input }),
  );
}

export function useUpdateHolidayCalendar() {
  return useCalendarMutation(({ id, ...body }: { id: string; name?: string; default?: boolean }) =>
    request<{ calendar: HolidayCalendar }>(`/holiday-calendars/${id}`, { method: "PATCH", body }),
  );
}

export function useDeleteHolidayCalendar() {
  return useCalendarMutation((id: string) => request<void>(`/holiday-calendars/${id}`, { method: "DELETE" }));
}

export function useSetHolidays() {
  return useCalendarMutation(({ id, days }: { id: string; days: Holiday[] }) =>
    request<{ calendar: HolidayCalendar }>(`/holiday-calendars/${id}/days`, { method: "PUT", body: { days } }),
  );
}

/** Adds the all-day events of an .ics file; a day the calendar has takes the file's name. */
export function useImportHolidays() {
  return useCalendarMutation(({ id, file }: { id: string; file: File }) => {
    const form = new FormData();
    form.append("file", file, file.name);
    return upload<{ calendar: HolidayCalendar; imported: number }>(`/holiday-calendars/${id}/import`, form);
  });
}

export function useWorkingWeek(userId: string | undefined) {
  return useQuery({
    queryKey: [...workingWeekQueryKey, userId],
    queryFn: () => request<{ week: WorkingWeek }>(`/users/${userId}/schedule`),
    enabled: Boolean(userId),
    // A portal customer has no week; asking again would not give them one.
    retry: false,
  });
}

export function useSetWorkingWeek() {
  return useCalendarMutation(({ userId, calendarId, minutes }: { userId: string; calendarId: string | null; minutes: Record<string, number> }) =>
    request<{ week: WorkingWeek }>(`/users/${userId}/schedule`, { method: "PUT", body: { calendarId, minutes } }),
  );
}

/** Days one person is away, both ends included. It says when and never why. */
export interface Absence {
  id: string;
  userId: string;
  userName: string;
  startsOn: string;
  endsOn: string;
  /** Half of a single day away. */
  halfDay: boolean;
}

/** A new absence, or what an edit changes. Left out, endsOn is startsOn and userId is the caller. */
export interface AbsenceInput {
  userId?: string;
  startsOn?: string;
  endsOn?: string;
  halfDay?: boolean;
}

const absencesQueryKey = ["absences"] as const;

/** Who is away; without from and to, the server's month back and year ahead. */
export function useAbsences(filter: { userId?: string; from?: string; to?: string } = {}) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filter)) if (value) params.set(key, value);
  const query = params.toString();
  return useQuery({
    queryKey: [...absencesQueryKey, filter],
    queryFn: () => request<{ absences: Absence[]; from: string; to: string }>(`/absences${query ? `?${query}` : ""}`),
    // A portal customer is refused; asking again would not let them in.
    retry: false,
  });
}

function useAbsenceMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: absencesQueryKey }),
  });
}

export function useRecordAbsence() {
  return useAbsenceMutation((input: AbsenceInput) => request<{ absence: Absence }>("/absences", { method: "POST", body: input }));
}

export function useUpdateAbsence() {
  return useAbsenceMutation(({ id, ...body }: Omit<AbsenceInput, "userId"> & { id: string }) =>
    request<{ absence: Absence }>(`/absences/${id}`, { method: "PATCH", body }),
  );
}

export function useRemoveAbsence() {
  return useAbsenceMutation((id: string) => request<void>(`/absences/${id}`, { method: "DELETE" }));
}
