import { useState, type FormEvent } from "react";
import { createRoute } from "@tanstack/react-router";
import { adminRoute } from "./admin";
import { useAccess } from "@/api/access";
import { useCreateHolidayCalendar, useDeleteHolidayCalendar, useHolidayCalendars, useUpdateHolidayCalendar, type HolidayCalendar } from "@/api/availability";
import { Button, Card, Dialog, ErrorBanner, Field, IconButton, Menu, Page, PageHeader, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HOLIDAY_NAME_MAX_LENGTH } from "@/config";
import { CalendarDays } from "@/features/availability/CalendarDays";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { t } from "@/i18n";

export const holidaysRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/holidays",
  component: HolidaysPage,
});

/**
 * The organization's calendars of days off. People in different places keep
 * different ones; whoever has not been given one keeps the default.
 */
function HolidaysPage() {
  const { data, isLoading, error } = useHolidayCalendars();
  const { data: access } = useAccess();
  const canAdminister = access?.canAdministerOrg ?? false;
  const calendars = data?.calendars ?? [];
  const [chosen, setChosen] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<HolidayCalendar | null>(null);
  const update = useUpdateHolidayCalendar();
  const remove = useDeleteHolidayCalendar();
  const confirm = useConfirm();
  const toast = useToast();
  const open = calendars.find((c) => c.id === chosen) ?? calendars[0];

  async function onDelete(calendar: HolidayCalendar) {
    const kept = calendar.desks.length === 0 ? "" : t.schedule.holidays.desksKeep(inWords(calendar.desks), calendar.desks.length);
    const body = t.schedule.holidays.deleteBody(calendar.name, calendar.dayCount, calendar.peopleCount, kept);
    if (await confirm({ noun: t.kit.confirm.things.calendar, verb: t.kit.confirm.verbs.delete, body })) {
      remove.mutate(calendar.id, {
        onSuccess: () => {
          setChosen(null);
          toast.success(t.schedule.holidays.deleted(calendar.name));
        },
      });
    }
  }

  return (
    <Page width="narrow">
      <PageHeader title={t.schedule.holidays.title} meta={t.schedule.holidays.meta} />
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
      {canAdminister && <NewCalendarForm onMade={setChosen} />}
      {isLoading ? null : (
        <Card>
          <Table>
            <thead>
              <tr>
                <Th>{t.schedule.holidays.calendar}</Th>
                <Th className="w-20 text-right">{t.schedule.holidays.days}</Th>
                <Th className="w-20 text-right">{t.schedule.holidays.people}</Th>
                <Th className="w-10" />
              </tr>
            </thead>
            <tbody>
              {calendars.map((calendar) => (
                <tr key={calendar.id} data-calendar-row={calendar.name} aria-current={calendar.id === open?.id || undefined}>
                  <Td>
                    <Button variant="link" onClick={() => setChosen(calendar.id)} data-action="open-calendar">
                      {calendar.name}
                    </Button>{" "}
                    {calendar.default && <Tag data-default-calendar>{t.schedule.holidays.default}</Tag>}
                  </Td>
                  <Td className="text-right">{calendar.dayCount}</Td>
                  <Td className="text-right" title={t.schedule.holidays.peopleHint}>
                    {calendar.peopleCount}
                  </Td>
                  <Td className="text-right">
                    {canAdminister && (
                      <Menu
                        label={t.schedule.releases.actionsFor(calendar.name)}
                        align="end"
                        trigger={(props) => (
                          <IconButton
                            icon={<Icon.More />}
                            label={t.schedule.releases.actionsFor(calendar.name)}
                            size="sm"
                            onClick={props.toggle}
                            aria-haspopup={props["aria-haspopup"]}
                            aria-expanded={props["aria-expanded"]}
                            data-action="calendar-menu"
                          />
                        )}
                        items={[
                          {
                            label: t.schedule.holidays.rename,
                            icon: <Icon.Edit />,
                            onSelect: () => setRenaming(calendar),
                            attrs: { "data-action": "rename-calendar" },
                          },
                          {
                            label: t.schedule.holidays.makeDefault,
                            icon: <Icon.Check />,
                            disabled: calendar.default,
                            onSelect: () =>
                              update.mutate(
                                { id: calendar.id, default: true },
                                { onSuccess: () => toast.success(t.schedule.holidays.nowDefault(calendar.name)) },
                              ),
                            attrs: { "data-action": "default-calendar" },
                          },
                          {
                            label: t.schedule.holidays.delete,
                            icon: <Icon.Trash />,
                            danger: true,
                            disabled: calendar.default,
                            onSelect: () => void onDelete(calendar),
                            attrs: { "data-action": "delete-calendar" },
                          },
                        ]}
                      />
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </Card>
      )}
      {open && <CalendarDays key={open.id} id={open.id} canAdminister={canAdminister} />}
      {renaming && <RenameDialog calendar={renaming} onClose={() => setRenaming(null)} />}
    </Page>
  );
}

/** Names in a sentence, as the server writes them: A, B and C. */
function inWords(names: string[]): string {
  return names.length < 2 ? names.join("") : `${names.slice(0, -1).join(", ")}${t.schedule.holidays.and}${names[names.length - 1]}`;
}

function NewCalendarForm({ onMade }: { onMade: (id: string) => void }) {
  const create = useCreateHolidayCalendar();
  const toast = useToast();
  const [name, setName] = useState("");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim() },
      {
        onSuccess: ({ calendar }) => {
          setName("");
          onMade(calendar.id);
          toast.success(t.schedule.holidays.made(calendar.name));
        },
      },
    );
  }

  return (
    <form onSubmit={onSubmit} className="mb-4 flex flex-wrap items-end gap-3" noValidate data-new-calendar>
      <Field
        label={t.schedule.holidays.newCalendar}
        placeholder={t.schedule.holidays.newPlaceholder}
        value={name}
        maxLength={HOLIDAY_NAME_MAX_LENGTH}
        onChange={(e) => setName(e.target.value)}
        className="w-64"
      />
      <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
        {t.schedule.holidays.add}
      </Button>
      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
    </form>
  );
}

function RenameDialog({ calendar, onClose }: { calendar: HolidayCalendar; onClose: () => void }) {
  const update = useUpdateHolidayCalendar();
  const [name, setName] = useState(calendar.name);
  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    update.mutate({ id: calendar.id, name: name.trim() }, { onSuccess: onClose });
  }
  return (
    <Dialog open onClose={onClose} title={t.schedule.holidays.renameTitle(calendar.name)}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label={t.schedule.holidays.name} autoFocus value={name} maxLength={HOLIDAY_NAME_MAX_LENGTH} onChange={(e) => setName(e.target.value)} />
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.schedule.holidays.cancel}
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            {t.schedule.holidays.rename}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
