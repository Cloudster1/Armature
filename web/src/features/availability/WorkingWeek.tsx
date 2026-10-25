import { useState, type FormEvent } from "react";
import { WEEKDAYS, useHolidayCalendars, useSetWorkingWeek, useWorkingWeek, type Weekday, type WorkingWeek } from "@/api/availability";
import { Button, Dialog, ErrorBanner, Input, Select, Table, Td, Th, useToast } from "@/components/ui";
import { HOURS_PER_DAY, WORKING_HOURS_STEP } from "@/config";
import { useFormat } from "@/lib/format";
import { hoursText, minutesFromHours, weekTotal, weekdayName } from "@/lib/week";
import { t } from "@/i18n";

/** A person's week as a table of hours, with the calendar whose days they have off. */
export function WorkingWeekTable({ week }: { week: WorkingWeek }) {
  const format = useFormat();
  return (
    <div className="space-y-3" data-working-week>
      <p className="text-sm text-ink-muted">
        {t.schedule.week.daysOffFrom}{" "}
        <span className="text-ink" data-week-calendar>
          {week.calendar?.name ?? t.schedule.week.noCalendar}
        </span>
        {week.calendarId === null && week.calendar ? t.schedule.week.everyonesDefault : ""}.{!week.saved && t.schedule.week.notSet}
      </p>
      <Table dense>
        <thead>
          <tr>
            {WEEKDAYS.map((day) => (
              <Th key={day} className="text-right">
                {weekdayName(day, format.locale, "short")}
              </Th>
            ))}
            <Th className="text-right">{t.schedule.week.week}</Th>
          </tr>
        </thead>
        <tbody>
          <tr>
            {WEEKDAYS.map((day) => (
              <Td key={day} className="text-right" data-week-hours={day}>
                {t.schedule.week.hours(hoursText(week.minutes[day] ?? 0))}
              </Td>
            ))}
            <Td className="text-right font-medium text-ink">{t.schedule.week.hours(hoursText(weekTotal(week.minutes)))}</Td>
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
    <Dialog
      open
      onClose={onClose}
      title={t.schedule.week.dialogTitle(name)}
      description={t.schedule.week.dialogDetail}
      attrs={{ "data-working-week-dialog": userId }}
    >
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
      {
        onSuccess: () => {
          toast.success(t.schedule.week.saved(name));
          onClose();
        },
      },
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-4" noValidate>
      <Select label={t.schedule.week.calendar} value={calendarId} onChange={(e) => setCalendarId(e.target.value)} data-week-calendar-select>
        <option value="">{t.schedule.week.theDefault(fallback?.name)}</option>
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
              aria-label={t.schedule.week.hoursOn(weekdayName(day, format.locale))}
              onChange={(e) => setHours({ ...hours, [day]: e.target.value })}
              className="text-right"
              data-week-day={day}
            />
          </label>
        ))}
      </div>
      <p className="text-sm text-ink-muted">
        {unreadable.length > 0
          ? t.schedule.week.unreadable(HOURS_PER_DAY, unreadable.map((day) => weekdayName(day, format.locale)).join(", "))
          : t.schedule.week.total(hoursText(weekTotal(minutes as Record<string, number>)))}
      </p>
      {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="secondary" onClick={onClose}>
          {t.schedule.week.cancel}
        </Button>
        <Button type="submit" loading={save.isPending} disabled={unreadable.length > 0} data-action="save-working-week">
          {t.schedule.week.save}
        </Button>
      </div>
    </form>
  );
}
