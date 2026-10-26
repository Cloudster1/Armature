import { useRef, useState } from "react";
import { useHolidayCalendar, useImportHolidays, useProposeNextYear, useSetHolidays, type Holiday, type NextYearProposal } from "@/api/availability";
import { Button, Card, Checkbox, EmptyState, ErrorBanner, IconButton, Input, SectionTitle, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HOLIDAY_FILE_ACCEPT, HOLIDAY_NAME_MAX_LENGTH } from "@/config";
import { useFormat } from "@/lib/format";
import { formatDay } from "@/lib/week";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

// A changed row counts once and a removed day once, so a table edited back to
// what is saved asks nothing.
function unsavedDays(draft: Holiday[], saved: Holiday[]): number {
  const same = (a: Holiday, b: Holiday) => a.day === b.day && a.name === b.name && a.halfDay === b.halfDay;
  const changed = draft.filter((row) => !saved.some((day) => same(day, row))).length;
  const removed = saved.filter((day) => !draft.some((row) => row.day === day.day)).length;
  return changed + removed;
}

/** The last year the days reach into, which is the one a copy carries on. */
export function latestYear(days: Holiday[]): number | null {
  const years = days.map((day) => Number.parseInt(day.day.slice(0, 4), 10)).filter((year) => !Number.isNaN(year));
  return years.length > 0 ? Math.max(...years) : null;
}

/**
 * One calendar's days off. An administrator edits them in place and saves the
 * whole list at once, or adds a file's worth from an .ics export.
 */
export function CalendarDays({ id, canAdminister }: { id: string; canAdminister: boolean }) {
  const { data, error } = useHolidayCalendar(id);
  const setDays = useSetHolidays();
  const importDays = useImportHolidays();
  const proposeNextYear = useProposeNextYear();
  const [copied, setCopied] = useState<NextYearProposal | null>(null);
  const toast = useToast();
  const confirm = useConfirm();
  const format = useFormat();
  const fileInput = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<Holiday[] | null>(null);
  const calendar = data?.calendar;
  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (!calendar) return null;
  const days = draft ?? calendar.days ?? [];
  const from = latestYear(calendar.days ?? []);

  // The copied days join whatever the table holds, typed or saved, and never
  // take a date it already has; nothing is written until Save.
  function copyNextYear() {
    if (!calendar) return;
    proposeNextYear.mutate(calendar.id, {
      onSuccess: ({ proposal }) => {
        const held = new Set(days.map((day) => day.day));
        const added = proposal.days.filter((day) => !held.has(day.day));
        setCopied({ ...proposal, days: added });
        if (added.length > 0) setDraft([...days, ...added].sort((a, b) => a.day.localeCompare(b.day)));
      },
    });
  }
  const discard = () => {
    setDraft(null);
    setCopied(null);
  };

  const change = (index: number, patch: Partial<Holiday>) => setDraft(days.map((day, i) => (i === index ? { ...day, ...patch } : day)));

  // The import replaces the table with what the server holds, so typed days
  // stay until the administrator agrees to lose them and the file has landed.
  async function onFile(file: File | undefined) {
    if (fileInput.current) fileInput.current.value = "";
    if (!file || !calendar) return;
    const unsaved = draft ? unsavedDays(draft, calendar.days ?? []) : 0;
    if (unsaved > 0) {
      const noun = t.kit.confirm.things.unsavedDays(unsaved);
      const body = t.schedule.holidays.importBody(file.name, calendar.name);
      if (!(await confirm({ noun, verb: t.kit.confirm.verbs.importAndDiscard, body }))) return;
    }
    importDays.mutate(
      { id: calendar.id, file },
      {
        onSuccess: ({ imported }) => {
          discard();
          toast.success(t.schedule.holidays.imported(imported, calendar.name));
        },
      },
    );
  }

  function save() {
    if (!draft || !calendar) return;
    setDays.mutate(
      { id: calendar.id, days: draft },
      {
        onSuccess: () => {
          discard();
          toast.success(t.schedule.holidays.savedDays(calendar.name));
        },
      },
    );
  }

  return (
    <section className="mt-8" data-holiday-calendar={calendar.name}>
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <SectionTitle>{t.schedule.holidays.daysOffIn(calendar.name)}</SectionTitle>
        {canAdminister && (
          <span className="flex gap-2">
            <Button
              size="sm"
              variant="secondary"
              icon={<Icon.Upload />}
              loading={importDays.isPending}
              onClick={() => fileInput.current?.click()}
              data-action="import-holidays"
            >
              {t.schedule.holidays.importIcs}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              icon={<Icon.Plus />}
              onClick={() => setDraft([...days, { day: "", name: "", halfDay: false }])}
              data-action="add-holiday"
            >
              {t.schedule.holidays.addDay}
            </Button>
            {from !== null && (
              <Button
                size="sm"
                variant="secondary"
                icon={<Icon.Copy />}
                loading={proposeNextYear.isPending}
                onClick={copyNextYear}
                data-action="copy-next-year"
              >
                {t.schedule.holidays.copyYear(from, from + 1)}
              </Button>
            )}
            <input
              ref={fileInput}
              type="file"
              accept={HOLIDAY_FILE_ACCEPT}
              className="hidden"
              aria-label={t.schedule.holidays.chooseFile}
              data-holiday-file
              onChange={(e) => void onFile(e.target.files?.[0])}
            />
          </span>
        )}
      </div>
      {importDays.error && <ErrorBanner>{(importDays.error as Error).message}</ErrorBanner>}
      {setDays.error && <ErrorBanner>{(setDays.error as Error).message}</ErrorBanner>}
      {proposeNextYear.error && <ErrorBanner>{(proposeNextYear.error as Error).message}</ErrorBanner>}
      {copied && (
        <p className="mb-2 text-sm text-ink-muted" data-holiday-copied={copied.days.length}>
          {copied.days.length > 0 ? t.schedule.holidays.copied(copied.days.length, copied.from, copied.to) : t.schedule.holidays.nothingToCopy(copied.to)}
          {copied.skipped.length > 0 && ` ${t.schedule.holidays.skippedLeapDay(copied.to)}`}
        </p>
      )}
      {days.length === 0 ? (
        <EmptyState title={t.schedule.holidays.empty} description={canAdminister ? t.schedule.holidays.emptyAdmin : t.schedule.holidays.emptyOthers} />
      ) : (
        <Card>
          <Table dense>
            <thead>
              <tr>
                <Th className="w-44">{t.schedule.holidays.day}</Th>
                <Th>{t.schedule.holidays.name}</Th>
                <Th className="w-28">{t.schedule.holidays.halfDay}</Th>
                {canAdminister && <Th className="w-10" />}
              </tr>
            </thead>
            <tbody>
              {days.map((day, index) =>
                canAdminister ? (
                  <tr key={index} data-holiday-day={day.day}>
                    <Td>
                      <Input
                        type="date"
                        controlSize="sm"
                        value={day.day}
                        aria-label={t.schedule.holidays.day}
                        onChange={(e) => change(index, { day: e.target.value })}
                      />
                    </Td>
                    <Td>
                      <Input
                        controlSize="sm"
                        value={day.name}
                        maxLength={HOLIDAY_NAME_MAX_LENGTH}
                        aria-label={t.schedule.holidays.name}
                        onChange={(e) => change(index, { name: e.target.value })}
                      />
                    </Td>
                    <Td>
                      <Checkbox
                        label={<span className="sr-only">{t.schedule.holidays.halfDay}</span>}
                        checked={day.halfDay}
                        onChange={(e) => change(index, { halfDay: e.target.checked })}
                      />
                    </Td>
                    <Td className="text-right">
                      <IconButton
                        icon={<Icon.X />}
                        label={t.schedule.holidays.removeDay(day.name)}
                        size="sm"
                        onClick={() => setDraft(days.filter((_, i) => i !== index))}
                      />
                    </Td>
                  </tr>
                ) : (
                  <tr key={day.day} data-holiday-day={day.day}>
                    <Td>{formatDay(day.day, format.locale)}</Td>
                    <Td className="text-ink">{day.name}</Td>
                    <Td>{day.halfDay && <Tag>{t.schedule.holidays.halfDay}</Tag>}</Td>
                  </tr>
                ),
              )}
            </tbody>
          </Table>
        </Card>
      )}
      {canAdminister && draft && (
        <div className="mt-3 flex justify-end gap-2">
          <Button variant="ghost" onClick={discard}>
            {t.schedule.holidays.discard}
          </Button>
          <Button loading={setDays.isPending} onClick={save} data-action="save-holidays">
            {t.schedule.holidays.saveDays}
          </Button>
        </div>
      )}
    </section>
  );
}
