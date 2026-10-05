import { useState, type FormEvent } from "react";
import { WEEKDAYS, useHolidayCalendars, useSetWorkingWeek, useWorkingWeek, type Weekday, type WorkingWeek } from "@/api/availability";
import { Button, Dialog, ErrorBanner, Input, Select, Table, Td, Th, useToast } from "@/components/ui";
import { HOURS_PER_DAY, WORKING_HOURS_STEP } from "@/config";
import { useFormat } from "@/lib/format";
import { hoursText, minutesFromHours, weekTotal, weekdayName } from "@/lib/week";

/** A person's week as a table of hours, with the calendar whose days they have off. */
export function WorkingWeekTable({ week }: { week: WorkingWeek }) {
  const format = useFormat();
  return (
    <div className="space-y-3" data-working-week>
      <p className="text-sm text-ink-muted">
        Days off from <span className="text-ink" data-week-calendar>{week.calendar?.name ?? "no calendar yet"}</span>
        {week.calendarId === null && week.calendar ? ", the organization's default" : ""}.
        {!week.saved && " Nobody has set your week, so it is the standard one."}
      </p>
      <Table dense>
        <thead>
          <tr>
            {WEEKDAYS.map((day) => (
              <Th key={day} className="text-right">
                {weekdayName(day, format.locale, "short")}
              </Th>
            ))}
            <Th className="text-right">Week</Th>
          </tr>
        </thead>
        <tbody>
          <tr>
            {WEEKDAYS.map((day) => (
              <Td key={day} className="text-right" data-week-hours={day}>
                {hoursText(week.minutes[day] ?? 0)} h
              </Td>
            ))}
            <Td className="text-right font-medium text-ink">{hoursText(weekTotal(week.minutes))} h</Td>
          </tr>
        </tbody>
      </Table>
    </div>
  );
}

/** An administrator sets somebody's hours per weekday and the calendar they keep. */
export function WorkingWeekDialog({ userId, name, onClose }: { userId: string; name: string; onClose: () => void }) {
  const { data, error } = useWorkingWeek(userId);
  return (
    <Dialog open onClose={onClose} title={`${name}'s working week`} description="Hours on each weekday, and the holiday calendar whose days they have off." attrs={{ "data-working-week-dialog": userId }}>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {data && <WorkingWeekForm week={data.week} name={name} onClose={onClose} />}
    </Dialog>
  );
}

function WorkingWeekForm({ week, name, onClose }: { week: WorkingWeek; name: string; onClose: () => void }) {
  const format = useFormat();
  const toast = useToast();
  const save = useSetWorkingWeek();
  const { data: calendars } = useHolidayCalendars();
  const [calendarId, setCalendarId] = useState(week.calendarId ?? "");
  const [hours, setHours] = useState<Record<Weekday, string>>(
    () => Object.fromEntries(WEEKDAYS.map((day) => [day, hoursText(week.minutes[day] ?? 0)])) as Record<Weekday, string>,
  );
  const minutes = Object.fromEntries(WEEKDAYS.map((day) => [day, minutesFromHours(hours[day])])) as Record<Weekday, number | null>;
  const unreadable = WEEKDAYS.filter((day) => minutes[day] === null);
  const fallback = calendars?.calendars.find((c) => c.default);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (unreadable.length > 0) return;
    save.mutate(
      { userId: week.userId, calendarId: calendarId || null, minutes: minutes as Record<string, number> },
      { onSuccess: () => { toast.success(`Saved ${name}'s working week`); onClose(); } },
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate>
      <Select label="Holiday calendar" value={calendarId} onChange={(e) => setCalendarId(e.target.value)} data-week-calendar-select>
        <option value="">The default{fallback ? ` (${fallback.name})` : ""}</option>
        {(calendars?.calendars ?? []).map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </Select>
      <div className="grid grid-cols-7 gap-2">
        {WEEKDAYS.map((day) => (
          <label key={day} className="space-y-1 text-center">
            <span className="block text-2xs font-medium tracking-wide text-ink-subtle uppercase">{weekdayName(day, format.locale, "short")}</span>
            <Input
              type="number"
              inputMode="decimal"
              min={0}
              max={HOURS_PER_DAY}
              step={WORKING_HOURS_STEP}
              value={hours[day]}
              invalid={minutes[day] === null}
              aria-label={`Hours on ${weekdayName(day, format.locale)}`}
              onChange={(e) => setHours({ ...hours, [day]: e.target.value })}
              className="text-right"
              data-week-day={day}
            />
          </label>
        ))}
      </div>
      <p className="text-sm text-ink-muted">
        {unreadable.length > 0
          ? `A day holds from 0 to ${HOURS_PER_DAY} hours. Fix ${unreadable.map((day) => weekdayName(day, format.locale)).join(", ")}.`
          : `${hoursText(weekTotal(minutes as Record<string, number>))} hours a week. Leave a day at 0 when it is not worked.`}
      </p>
      {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="secondary" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" loading={save.isPending} disabled={unreadable.length > 0} data-action="save-working-week">
          Save week
        </Button>
      </div>
    </form>
  );
}
