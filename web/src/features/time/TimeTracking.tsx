import { useState, type FormEvent } from "react";
import { useDeleteWorklog, useLogWork, useUpdateIssue, useUpdateWorklog, useWorklogs, type Issue, type Worklog } from "@/api/issues";
import { Button, cx, ErrorBanner, Field, Input, Labelled } from "@/components/ui";
import { canAdminister, useAccess } from "@/api/access";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { todayIn, useFormat } from "@/lib/format";
import { Avatar } from "@/features/issues/badges";
import { formatDuration, parseDuration, timeProgress } from "./duration";
import { formatDay } from "@/lib/week";
import { t } from "@/i18n";
import { APP_NAME } from "@/config";

/**
 * The time on an issue as the side panel shows it: the estimate, what is
 * left, how much was spent, and a bar of the three. Estimate and remaining
 * are typed the way people write time; the server keeps minutes.
 */
export function TimeSummary({ issue, editable }: { issue: Issue; editable: boolean }) {
  const progress = timeProgress(issue.timeSpentMinutes, issue.timeRemainingMinutes, issue.timeEstimateMinutes);
  const over = issue.timeEstimateMinutes !== undefined && issue.timeSpentMinutes > issue.timeEstimateMinutes;
  return (
    <div className="w-full space-y-1.5" data-time-tracking>
      <div className="h-1.5 w-full overflow-hidden rounded bg-surface-raised">
        <div
          className={cx("h-full rounded", over ? "bg-danger" : "bg-accent")}
          style={{ width: `${Math.round(progress * 100)}%` }}
          data-time-progress={Math.round(progress * 100)}
        />
      </div>
      <dl className="grid grid-cols-3 gap-2 text-xs">
        <TimeCell label={t.issuePanels.time.estimated} minutes={issue.timeEstimateMinutes} field="timeEstimateMinutes" issue={issue} editable={editable} />
        <TimeCell label={t.issuePanels.time.remaining} minutes={issue.timeRemainingMinutes} field="timeRemainingMinutes" issue={issue} editable={editable} />
        <div>
          <dt className="text-ink-subtle">{t.issuePanels.time.spent}</dt>
          <dd className="text-ink" data-time-spent>
            {formatDuration(issue.timeSpentMinutes) || "0m"}
          </dd>
        </div>
      </dl>
    </div>
  );
}

function TimeCell({
  label,
  minutes,
  field,
  issue,
  editable,
}: {
  label: string;
  minutes: number | undefined;
  field: "timeEstimateMinutes" | "timeRemainingMinutes";
  issue: Issue;
  editable: boolean;
}) {
  const update = useUpdateIssue();
  const [text, setText] = useState<string | null>(null);
  const [refused, setRefused] = useState<string | null>(null);
  const shown = text ?? formatDuration(minutes);

  function save() {
    if (text === null) return;
    const parsed = parseDuration(text);
    if (parsed === null) {
      setRefused(t.issuePanels.time.notTime(label));
      return;
    }
    setRefused(null);
    setText(null);
    if (parsed === (minutes ?? 0) && (parsed !== 0 || minutes === undefined)) return;
    update.mutate({ key: issue.key, [field]: parsed === 0 ? null : parsed });
  }

  return (
    <div>
      <dt className="text-ink-subtle">{label}</dt>
      <dd className="text-ink">
        {editable ? (
          <Input
            aria-label={label}
            value={shown}
            placeholder={t.issuePanels.time.none}
            onChange={(e) => setText(e.target.value)}
            onBlur={save}
            onKeyDown={(e) => {
              if (e.key === "Enter") (e.target as HTMLInputElement).blur();
            }}
            controlSize="sm"
          />
        ) : (
          <span>{formatDuration(minutes) || t.issuePanels.time.none}</span>
        )}
        {refused && <p className="mt-1 text-2xs text-danger">{refused}</p>}
      </dd>
    </div>
  );
}

/** The work logged on an issue, and a form to log more. */
export function WorklogPanel({ issue, editable, me }: { issue: Issue; editable: boolean; me?: string }) {
  const { data } = useWorklogs(issue.key);
  const { data: access } = useAccess();
  // An entry is its author's to correct or remove, or a project administrator's.
  const mayChange = (authorID?: string) => editable && (authorID === me || canAdminister(access, issue.projectKey));
  const log = useLogWork();
  const remove = useDeleteWorklog();
  const confirm = useConfirm();
  const { locale } = useFormat();
  const [duration, setDuration] = useState("");
  const { timezone } = useFormat();
  const [day, setDay] = useState(() => todayIn(timezone));
  const [note, setNote] = useState("");
  const [refused, setRefused] = useState<string | null>(null);
  const [editingID, setEditingID] = useState<string | null>(null);

  const worklogs = data?.worklogs ?? [];
  if (worklogs.length === 0 && !editable) return null;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const minutes = parseDuration(duration);
    if (minutes === null || minutes <= 0) {
      setRefused(t.issuePanels.time.sayHowLong);
      return;
    }
    setRefused(null);
    log.mutate(
      { key: issue.key, minutes, startedOn: day, note },
      {
        onSuccess: () => {
          setDuration("");
          setNote("");
        },
      },
    );
  }

  const error = refused ?? (log.error as Error | null)?.message ?? (remove.error as Error | null)?.message;

  return (
    <section data-worklogs>
      <h2 className="mb-3 text-xs font-semibold tracking-wide text-ink-muted uppercase">
        {t.issuePanels.time.workLog} {worklogs.length > 0 && `(${formatDuration(issue.timeSpentMinutes)})`}
      </h2>
      {worklogs.length > 0 && (
        <ul className="mb-4 divide-y divide-border rounded-md border border-border">
          {worklogs.map((w) => (
            <li key={w.id} className="flex items-center gap-3 px-3 py-2 text-sm" data-worklog={w.id}>
              {editingID === w.id ? (
                <WorklogEditor worklog={w} close={() => setEditingID(null)} />
              ) : (
                <>
                  <Avatar name={w.author?.name} src={w.author?.avatarUrl} size="sm" />
                  <span className="w-16 shrink-0 font-medium text-ink tabular-nums">{formatDuration(w.minutes)}</span>
                  <span className="min-w-0 flex-1 truncate text-ink-muted">
                    {w.note || <span className="text-ink-subtle">{t.issuePanels.time.noNote}</span>}
                  </span>
                  <span className="shrink-0 text-sm text-ink-subtle">
                    {w.author?.name ?? APP_NAME} · {formatDay(w.startedOn.slice(0, 10), locale)}
                  </span>
                  {mayChange(w.author?.id) && (
                    <>
                      <Button
                        size="sm"
                        variant="ghost"
                        aria-label={t.issuePanels.time.editWorklog(formatDuration(w.minutes))}
                        onClick={() => setEditingID(w.id)}
                        data-action="edit-worklog"
                      >
                        {t.issuePanels.time.edit}
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        aria-label={t.issuePanels.time.removeWorklog(formatDuration(w.minutes))}
                        onClick={async () =>
                          (await confirm({ noun: "work log", verb: "Remove", body: t.issuePanels.time.comesOff(formatDuration(w.minutes)) })) &&
                          remove.mutate(w.id)
                        }
                      >
                        {t.issuePanels.common.remove}
                      </Button>
                    </>
                  )}
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      {editable && (
        <form onSubmit={onSubmit} className="flex flex-wrap items-end gap-2">
          <Field
            label={t.issuePanels.time.timeSpent}
            id="worklog-duration"
            value={duration}
            onChange={(e) => setDuration(e.target.value)}
            placeholder="2h 30m"
            className="w-28"
          />
          <Field label={t.issuePanels.time.on} id="worklog-day" type="date" value={day} onChange={(e) => setDay(e.target.value)} />
          <Labelled id="worklog-note" label={t.issuePanels.time.whatWasDone} className="min-w-40 flex-1">
            <Input id="worklog-note" value={note} onChange={(e) => setNote(e.target.value)} placeholder={t.issuePanels.time.optional} />
          </Labelled>
          <Button type="submit" size="sm" loading={log.isPending} disabled={!duration.trim()}>
            {t.issuePanels.time.logWork}
          </Button>
        </form>
      )}
      {error && (
        <div className="mt-2">
          <ErrorBanner>{error}</ErrorBanner>
        </div>
      )}
      {worklogs.length === 0 && editable && !error && <p className="mt-2 text-sm text-ink-subtle">{t.issuePanels.time.nothingLogged}</p>}
    </section>
  );
}

/**
 * One entry corrected in place. The server replaces the whole entry, so the
 * duration, the day and the note are always sent together.
 */
function WorklogEditor({ worklog, close }: { worklog: Worklog; close: () => void }) {
  const update = useUpdateWorklog();
  const [duration, setDuration] = useState(formatDuration(worklog.minutes));
  const [day, setDay] = useState(worklog.startedOn.slice(0, 10));
  const [note, setNote] = useState(worklog.note);
  const [refused, setRefused] = useState<string | null>(null);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const minutes = parseDuration(duration);
    if (minutes === null || minutes <= 0) {
      setRefused(t.issuePanels.time.sayHowLong);
      return;
    }
    setRefused(null);
    update.mutate({ id: worklog.id, minutes, startedOn: day, note }, { onSuccess: close });
  }

  const error = refused ?? (update.error as Error | null)?.message;
  const prefix = `worklog-${worklog.id}`;

  return (
    <form onSubmit={onSubmit} className="w-full space-y-2" data-worklog-editor>
      <div className="flex flex-wrap items-end gap-2">
        <Field label={t.issuePanels.time.timeSpent} id={`${prefix}-duration`} value={duration} onChange={(e) => setDuration(e.target.value)} className="w-28" />
        <Field label={t.issuePanels.time.on} id={`${prefix}-day`} type="date" value={day} onChange={(e) => setDay(e.target.value)} />
        <Labelled id={`${prefix}-note`} label={t.issuePanels.time.whatWasDone} className="min-w-40 flex-1">
          <Input id={`${prefix}-note`} value={note} onChange={(e) => setNote(e.target.value)} placeholder={t.issuePanels.time.optional} />
        </Labelled>
        <Button type="submit" size="sm" loading={update.isPending} disabled={!duration.trim()} data-action="save-worklog">
          {t.issuePanels.common.save}
        </Button>
        <Button size="sm" variant="ghost" onClick={close}>
          {t.issuePanels.common.cancel}
        </Button>
      </div>
      {error && <ErrorBanner>{error}</ErrorBanner>}
    </form>
  );
}
