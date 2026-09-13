import { useState, type FormEvent } from "react";
import {
  useDeleteWorklog,
  useLogWork,
  useUpdateIssue,
  useWorklogs,
  type Issue,
} from "@/api/issues";
import { Button, cx, ErrorBanner, Field, Input, Labelled } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { Avatar } from "@/features/issues/badges";
import { formatDuration, parseDuration, timeProgress } from "./duration";


/**
 * The time on an issue as the side panel shows it: the estimate, what is
 * left, how much was spent, and a bar of the three. Estimate and remaining
 * are typed the way people write time; the server keeps minutes.
 */
export function TimeSummary({ issue, editable }: { issue: Issue; editable: boolean }) {
  const progress = timeProgress(issue.timeSpentMinutes, issue.timeRemainingMinutes, issue.timeEstimateMinutes);
  const over =
    issue.timeEstimateMinutes !== undefined && issue.timeSpentMinutes > issue.timeEstimateMinutes;
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
        <TimeCell label="Estimated" minutes={issue.timeEstimateMinutes} field="timeEstimateMinutes" issue={issue} editable={editable} />
        <TimeCell label="Remaining" minutes={issue.timeRemainingMinutes} field="timeRemainingMinutes" issue={issue} editable={editable} />
        <div>
          <dt className="text-ink-subtle">Spent</dt>
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
      setRefused(`${label} takes time such as 2h 30m, 1d or 45m.`);
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
            placeholder="none"
            onChange={(e) => setText(e.target.value)}
            onBlur={save}
            onKeyDown={(e) => {
              if (e.key === "Enter") (e.target as HTMLInputElement).blur();
            }}
            controlSize="sm"
          />
        ) : (
          <span>{formatDuration(minutes) || "none"}</span>
        )}
        {refused && <p className="mt-1 text-2xs text-danger">{refused}</p>}
      </dd>
    </div>
  );
}

/** The work logged on an issue, and a form to log more. */
export function WorklogPanel({ issue, editable, me }: { issue: Issue; editable: boolean; me?: string }) {
  const { data } = useWorklogs(issue.key);
  const log = useLogWork();
  const remove = useDeleteWorklog();
  const confirm = useConfirm();
  const [duration, setDuration] = useState("");
  const [day, setDay] = useState(() => new Date().toISOString().slice(0, 10));
  const [note, setNote] = useState("");
  const [refused, setRefused] = useState<string | null>(null);

  const worklogs = data?.worklogs ?? [];
  if (worklogs.length === 0 && !editable) return null;

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const minutes = parseDuration(duration);
    if (minutes === null || minutes <= 0) {
      setRefused("Say how long, such as 2h 30m, 1d or 45m.");
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
        Work log {worklogs.length > 0 && `(${formatDuration(issue.timeSpentMinutes)})`}
      </h2>
      {worklogs.length > 0 && (
        <ul className="mb-4 divide-y divide-border rounded-md border border-border">
          {worklogs.map((w) => (
            <li key={w.id} className="flex items-center gap-3 px-3 py-2 text-sm" data-worklog={w.id}>
              <Avatar name={w.author?.name} src={w.author?.avatarUrl} size="sm" />
              <span className="w-16 shrink-0 font-medium text-ink tabular-nums">{formatDuration(w.minutes)}</span>
              <span className="min-w-0 flex-1 truncate text-ink-muted">{w.note || <span className="text-ink-subtle">No note</span>}</span>
              <span className="shrink-0 text-sm text-ink-subtle">
                {w.author?.name ?? "Armature"} · {w.startedOn.slice(0, 10)}
              </span>
              {editable && w.author?.id === me && (
                <Button size="sm" variant="ghost" aria-label={`Remove work log ${formatDuration(w.minutes)}`} onClick={async () => (await confirm({ noun: "work log", verb: "Remove", body: `${formatDuration(w.minutes)} comes off the time spent.` })) && remove.mutate(w.id)}>
                  Remove
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {editable && (
        <form onSubmit={onSubmit} className="flex flex-wrap items-end gap-2">
          <Field label="Time spent" id="worklog-duration" value={duration} onChange={(e) => setDuration(e.target.value)} placeholder="2h 30m" className="w-28" />
          <Field label="On" id="worklog-day" type="date" value={day} onChange={(e) => setDay(e.target.value)} />
          <Labelled id="worklog-note" label="What was done" className="min-w-40 flex-1">
            <Input id="worklog-note" value={note} onChange={(e) => setNote(e.target.value)} placeholder="Optional" />
          </Labelled>
          <Button type="submit" size="sm" loading={log.isPending} disabled={!duration.trim()}>
            Log work
          </Button>
        </form>
      )}
      {error && (
        <div className="mt-2">
          <ErrorBanner>{error}</ErrorBanner>
        </div>
      )}
      {worklogs.length === 0 && editable && !error && (
        <p className="mt-2 text-sm text-ink-subtle">Nothing logged yet.</p>
      )}
    </section>
  );
}
