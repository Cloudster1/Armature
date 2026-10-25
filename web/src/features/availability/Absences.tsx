import { useState, type FormEvent } from "react";
import { useAbsences, useRecordAbsence, useRemoveAbsence, useUpdateAbsence, type Absence } from "@/api/availability";
import { Button, Checkbox, Dialog, ErrorBanner, Field, Tag, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useFormat } from "@/lib/format";
import { absenceProblem, absencesListedFrom, formatAbsence, today } from "@/lib/week";
import { t } from "@/i18n";

/**
 * Somebody's absences with a form to add one, and each to change or take back.
 * There is nowhere to say why: colleagues see the days and nothing else.
 */
export function AbsenceEditor({ userId, absences, empty, truncated = false }: { userId: string; absences: Absence[]; empty: string; truncated?: boolean }) {
  const remove = useRemoveAbsence();
  const confirm = useConfirm();
  const toast = useToast();
  const format = useFormat();
  const [editing, setEditing] = useState<Absence | null>(null);
  const now = today();

  return (
    <div className="space-y-4" data-absences={userId}>
      <AbsenceForm key={editing?.id ?? "new"} userId={userId} editing={editing} onDone={() => setEditing(null)} />
      {absences.length === 0 ? (
        <p className="text-sm text-ink-subtle">{empty}</p>
      ) : (
        <ul className="divide-y divide-border border-t border-border" data-absence-list>
          {absences.map((absence) => (
            <li key={absence.id} className="flex flex-wrap items-center gap-2 py-2 text-sm" data-absence={absence.startsOn}>
              <span className={absence.endsOn < now ? "text-ink-subtle" : "text-ink"}>{formatAbsence(absence, format.locale)}</span>
              {absence.endsOn < now && <Tag>{t.schedule.absences.past}</Tag>}
              <span className="ml-auto flex gap-1">
                <Button size="sm" variant="ghost" onClick={() => setEditing(absence)} data-action="edit-absence">
                  {t.schedule.absences.change}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  data-action="remove-absence"
                  onClick={async () => {
                    if (
                      !(await confirm({
                        noun: t.kit.confirm.things.absence,
                        verb: t.kit.confirm.verbs.remove,
                        body: t.schedule.absences.removeBody(formatAbsence(absence, format.locale)),
                      }))
                    )
                      return;
                    remove.mutate(absence.id, { onSuccess: () => toast.success(t.schedule.absences.removed) });
                    if (editing?.id === absence.id) setEditing(null);
                  }}
                >
                  {t.schedule.absences.remove}
                </Button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {truncated && (
        <p className="text-sm text-ink-muted" data-absences-truncated>
          {t.schedule.absences.truncated}
        </p>
      )}
      {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
    </div>
  );
}

/** A first and a last day, both away, and whether a single day is only half. */
function AbsenceForm({ userId, editing, onDone }: { userId: string; editing: Absence | null; onDone: () => void }) {
  const record = useRecordAbsence();
  const update = useUpdateAbsence();
  const toast = useToast();
  const [startsOn, setStartsOn] = useState(editing?.startsOn ?? "");
  const [endsOn, setEndsOn] = useState(editing?.endsOn ?? "");
  const [halfDay, setHalfDay] = useState(editing?.halfDay ?? false);
  const last = endsOn || startsOn;
  const problem = startsOn ? absenceProblem(startsOn, last, halfDay) : null;
  const failed = (record.error ?? update.error) as Error | null;

  function done(message: string) {
    toast.success(message);
    setStartsOn("");
    setEndsOn("");
    setHalfDay(false);
    onDone();
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!startsOn || problem) return;
    const days = { startsOn, endsOn: last, halfDay };
    if (editing) update.mutate({ id: editing.id, ...days }, { onSuccess: () => done(t.schedule.absences.changed) });
    else record.mutate({ userId, ...days }, { onSuccess: () => done(t.schedule.absences.recorded) });
  }

  return (
    <form onSubmit={onSubmit} className="space-y-3" noValidate data-absence-form>
      <div className="flex flex-wrap items-end gap-3">
        <Field
          label={t.schedule.absences.firstDay}
          id={`absence-from-${userId}`}
          type="date"
          value={startsOn}
          onChange={(e) => setStartsOn(e.target.value)}
          data-absence-from
          className="w-44"
        />
        <Field
          label={t.schedule.absences.lastDay}
          id={`absence-to-${userId}`}
          type="date"
          value={endsOn}
          min={startsOn || undefined}
          onChange={(e) => setEndsOn(e.target.value)}
          hint={t.schedule.absences.lastDayHint}
          data-absence-to
          className="w-44"
        />
        <Checkbox label={t.schedule.absences.halfDay} checked={halfDay} onChange={(e) => setHalfDay(e.target.checked)} data-absence-half />
      </div>
      {problem && (
        <p className="text-sm text-danger" role="alert">
          {problem}
        </p>
      )}
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      <div className="flex gap-2">
        <Button type="submit" loading={record.isPending || update.isPending} disabled={!startsOn || Boolean(problem)} data-action="record-absence">
          {editing ? t.schedule.absences.save : t.schedule.absences.record}
        </Button>
        {editing && (
          <Button type="button" variant="secondary" onClick={onDone}>
            {t.schedule.absences.cancel}
          </Button>
        )}
      </div>
    </form>
  );
}

/** Somebody who plans with a person records the days they are away, from the teams page. */
export function AbsenceDialog({ userId, name, onClose }: { userId: string; name: string; onClose: () => void }) {
  const { data, error } = useAbsences({ userId, from: absencesListedFrom() });
  return (
    <Dialog
      open
      onClose={onClose}
      title={t.schedule.absences.dialogTitle(name)}
      description={t.schedule.absences.dialogDetail}
      attrs={{ "data-absence-dialog": userId }}
    >
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {data && <AbsenceEditor userId={userId} absences={data.absences} truncated={data.truncated} empty={t.schedule.absences.notAway(name)} />}
    </Dialog>
  );
}
