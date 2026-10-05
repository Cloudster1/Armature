import { useState, type FormEvent } from "react";
import { useAbsences, useRecordAbsence, useRemoveAbsence, useUpdateAbsence, type Absence } from "@/api/availability";
import { Button, Checkbox, Dialog, ErrorBanner, Field, Tag, useToast } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useFormat } from "@/lib/format";
import { absenceProblem, formatAbsence, today } from "@/lib/week";

/**
 * Somebody's absences with a form to add one, and each to change or take back.
 * There is nowhere to say why: colleagues see the days and nothing else.
 */
export function AbsenceEditor({ userId, absences, empty }: { userId: string; absences: Absence[]; empty: string }) {
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
              {absence.endsOn < now && <Tag>Past</Tag>}
              <span className="ml-auto flex gap-1">
                <Button size="sm" variant="ghost" onClick={() => setEditing(absence)} data-action="edit-absence">
                  Change
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  data-action="remove-absence"
                  onClick={async () => {
                    if (!(await confirm({ noun: "absence", verb: "Remove", body: `${formatAbsence(absence, format.locale)} is no longer marked as away.` }))) return;
                    remove.mutate(absence.id, { onSuccess: () => toast.success("Absence removed") });
                    if (editing?.id === absence.id) setEditing(null);
                  }}
                >
                  Remove
                </Button>
              </span>
            </li>
          ))}
        </ul>
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
    if (editing) update.mutate({ id: editing.id, ...days }, { onSuccess: () => done("Absence changed") });
    else record.mutate({ userId, ...days }, { onSuccess: () => done("Absence recorded") });
  }

  return (
    <form onSubmit={onSubmit} className="space-y-3" noValidate data-absence-form>
      <div className="flex flex-wrap items-end gap-3">
        <Field label="First day" id={`absence-from-${userId}`} type="date" value={startsOn} onChange={(e) => setStartsOn(e.target.value)} data-absence-from className="w-44" />
        <Field label="Last day" id={`absence-to-${userId}`} type="date" value={endsOn} min={startsOn || undefined} onChange={(e) => setEndsOn(e.target.value)} hint="Leave empty for one day." data-absence-to className="w-44" />
        <Checkbox label="Half of the day" checked={halfDay} onChange={(e) => setHalfDay(e.target.checked)} data-absence-half />
      </div>
      {problem && <p className="text-sm text-danger" role="alert">{problem}</p>}
      {failed && <ErrorBanner>{failed.message}</ErrorBanner>}
      <div className="flex gap-2">
        <Button type="submit" loading={record.isPending || update.isPending} disabled={!startsOn || Boolean(problem)} data-action="record-absence">
          {editing ? "Save absence" : "Record absence"}
        </Button>
        {editing && (
          <Button type="button" variant="secondary" onClick={onDone}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}

/** Somebody who plans with a person records the days they are away, from the teams page. */
export function AbsenceDialog({ userId, name, onClose }: { userId: string; name: string; onClose: () => void }) {
  const { data, error } = useAbsences({ userId });
  return (
    <Dialog open onClose={onClose} title={`${name} is away`} description="The days only. There is nowhere to say why, and colleagues see nothing more." attrs={{ "data-absence-dialog": userId }}>
      {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
      {data && <AbsenceEditor userId={userId} absences={data.absences} empty={`${name} is not away in the last month or the year ahead.`} />}
    </Dialog>
  );
}
