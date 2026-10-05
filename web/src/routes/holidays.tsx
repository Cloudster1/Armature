import { useState, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { useCreateHolidayCalendar, useDeleteHolidayCalendar, useHolidayCalendars, useUpdateHolidayCalendar, type HolidayCalendar } from "@/api/availability";
import { Button, Card, Dialog, ErrorBanner, Field, IconButton, Menu, Page, PageHeader, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HOLIDAY_NAME_MAX_LENGTH } from "@/config";
import { CalendarDays } from "@/features/availability/CalendarDays";
import { useConfirm } from "@/features/shell/ConfirmProvider";

export const holidaysRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/holidays",
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
    const body = `${calendar.name} and its ${calendar.dayCount} days go. The ${calendar.peopleCount} people given it keep the default instead.`;
    if (await confirm({ noun: "calendar", verb: "Delete", body })) {
      remove.mutate(calendar.id, { onSuccess: () => { setChosen(null); toast.success(`Deleted ${calendar.name}`); } });
    }
  }

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Holidays"
        meta="Calendars of days off. Everybody keeps the default until given another under Users, where their working week is set too."
      />
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
      {canAdminister && <NewCalendarForm onMade={setChosen} />}
      {isLoading ? null : (
        <Card>
          <Table>
            <thead>
              <tr>
                <Th>Calendar</Th>
                <Th className="w-20 text-right">Days</Th>
                <Th className="w-20 text-right">People</Th>
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
                    {calendar.default && <Tag data-default-calendar>Default</Tag>}
                  </Td>
                  <Td className="text-right">{calendar.dayCount}</Td>
                  <Td className="text-right" title="Given this calendar by name; the rest of the organization keeps the default.">
                    {calendar.peopleCount}
                  </Td>
                  <Td className="text-right">
                    {canAdminister && (
                      <Menu
                        label={`Actions for ${calendar.name}`}
                        align="end"
                        trigger={(props) => (
                          <IconButton
                            icon={<Icon.More />}
                            label={`Actions for ${calendar.name}`}
                            size="sm"
                            onClick={props.toggle}
                            aria-haspopup={props["aria-haspopup"]}
                            aria-expanded={props["aria-expanded"]}
                            data-action="calendar-menu"
                          />
                        )}
                        items={[
                          { label: "Rename", icon: <Icon.Edit />, onSelect: () => setRenaming(calendar), attrs: { "data-action": "rename-calendar" } },
                          {
                            label: "Make the default",
                            icon: <Icon.Check />,
                            disabled: calendar.default,
                            onSelect: () => update.mutate({ id: calendar.id, default: true }, { onSuccess: () => toast.success(`${calendar.name} is now the default`) }),
                            attrs: { "data-action": "default-calendar" },
                          },
                          { label: "Delete", icon: <Icon.Trash />, danger: true, disabled: calendar.default, onSelect: () => void onDelete(calendar), attrs: { "data-action": "delete-calendar" } },
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
          toast.success(`Made ${calendar.name}`);
        },
      },
    );
  }

  return (
    <form onSubmit={onSubmit} className="mb-4 flex flex-wrap items-end gap-3" noValidate data-new-calendar>
      <Field label="New calendar" placeholder="Portugal, or Office in Lisbon" value={name} maxLength={HOLIDAY_NAME_MAX_LENGTH} onChange={(e) => setName(e.target.value)} className="w-64" />
      <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
        Add calendar
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
    <Dialog open onClose={onClose} title={`Rename ${calendar.name}`}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label="Name" autoFocus value={name} maxLength={HOLIDAY_NAME_MAX_LENGTH} onChange={(e) => setName(e.target.value)} />
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
            Rename
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
