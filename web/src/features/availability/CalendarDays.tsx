import { useRef, useState } from "react";
import { useHolidayCalendar, useImportHolidays, useSetHolidays, type Holiday } from "@/api/availability";
import { Button, Card, Checkbox, EmptyState, ErrorBanner, IconButton, Input, SectionTitle, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HOLIDAY_FILE_ACCEPT, HOLIDAY_NAME_MAX_LENGTH } from "@/config";
import { useFormat } from "@/lib/format";
import { formatDay } from "@/lib/week";

/**
 * One calendar's days off. An administrator edits them in place and saves the
 * whole list at once, or adds a file's worth from an .ics export.
 */
export function CalendarDays({ id, canAdminister }: { id: string; canAdminister: boolean }) {
  const { data, error } = useHolidayCalendar(id);
  const setDays = useSetHolidays();
  const importDays = useImportHolidays();
  const toast = useToast();
  const format = useFormat();
  const fileInput = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<Holiday[] | null>(null);
  const calendar = data?.calendar;
  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (!calendar) return null;
  const days = draft ?? calendar.days ?? [];

  const change = (index: number, patch: Partial<Holiday>) => setDraft(days.map((day, i) => (i === index ? { ...day, ...patch } : day)));

  function onFile(file: File | undefined) {
    if (!file || !calendar) return;
    importDays.mutate(
      { id: calendar.id, file },
      {
        onSuccess: ({ imported }) => {
          setDraft(null);
          toast.success(`Added ${imported} ${imported === 1 ? "day" : "days"} to ${calendar.name}`);
        },
      },
    );
    if (fileInput.current) fileInput.current.value = "";
  }

  function save() {
    if (!draft || !calendar) return;
    setDays.mutate({ id: calendar.id, days: draft }, { onSuccess: () => { setDraft(null); toast.success(`Saved the days of ${calendar.name}`); } });
  }

  return (
    <section className="mt-8" data-holiday-calendar={calendar.name}>
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <SectionTitle>Days off in {calendar.name}</SectionTitle>
        {canAdminister && (
          <span className="flex gap-2">
            <Button size="sm" variant="secondary" icon={<Icon.Upload />} loading={importDays.isPending} onClick={() => fileInput.current?.click()} data-action="import-holidays">
              Import .ics
            </Button>
            <Button size="sm" variant="secondary" icon={<Icon.Plus />} onClick={() => setDraft([...days, { day: "", name: "", halfDay: false }])} data-action="add-holiday">
              Add a day
            </Button>
            <input ref={fileInput} type="file" accept={HOLIDAY_FILE_ACCEPT} className="hidden" aria-label="Choose an .ics file" data-holiday-file onChange={(e) => onFile(e.target.files?.[0])} />
          </span>
        )}
      </div>
      {importDays.error && <ErrorBanner>{(importDays.error as Error).message}</ErrorBanner>}
      {setDays.error && <ErrorBanner>{(setDays.error as Error).message}</ErrorBanner>}
      {days.length === 0 ? (
        <EmptyState
          title="No days off yet"
          description={canAdminister ? "Add the days by hand, or import the .ics file a calendar application exports. Recurring events need to be exported as separate days." : "An administrator adds the days off here."}
        />
      ) : (
        <Card>
          <Table dense>
            <thead>
              <tr>
                <Th className="w-44">Day</Th>
                <Th>Name</Th>
                <Th className="w-28">Half day</Th>
                {canAdminister && <Th className="w-10" />}
              </tr>
            </thead>
            <tbody>
              {days.map((day, index) =>
                canAdminister ? (
                  <tr key={index} data-holiday-day={day.day}>
                    <Td>
                      <Input type="date" controlSize="sm" value={day.day} aria-label="Day" onChange={(e) => change(index, { day: e.target.value })} />
                    </Td>
                    <Td>
                      <Input controlSize="sm" value={day.name} maxLength={HOLIDAY_NAME_MAX_LENGTH} aria-label="Name" onChange={(e) => change(index, { name: e.target.value })} />
                    </Td>
                    <Td>
                      <Checkbox label={<span className="sr-only">Half day</span>} checked={day.halfDay} onChange={(e) => change(index, { halfDay: e.target.checked })} />
                    </Td>
                    <Td className="text-right">
                      <IconButton icon={<Icon.X />} label={`Remove ${day.name || "this day"}`} size="sm" onClick={() => setDraft(days.filter((_, i) => i !== index))} />
                    </Td>
                  </tr>
                ) : (
                  <tr key={day.day} data-holiday-day={day.day}>
                    <Td>{formatDay(day.day, format.locale)}</Td>
                    <Td className="text-ink">{day.name}</Td>
                    <Td>{day.halfDay && <Tag>Half day</Tag>}</Td>
                  </tr>
                ),
              )}
            </tbody>
          </Table>
        </Card>
      )}
      {canAdminister && draft && (
        <div className="mt-3 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setDraft(null)}>
            Discard changes
          </Button>
          <Button loading={setDays.isPending} onClick={save} data-action="save-holidays">
            Save days
          </Button>
        </div>
      )}
    </section>
  );
}
