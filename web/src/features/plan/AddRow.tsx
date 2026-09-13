import { useEffect, useRef, type KeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import type { IssueType } from "@/api/issues";
import { Input, SelectInput } from "@/components/ui";
import { PLAN_INDENT_PER_LEVEL, PLAN_ROW_HEIGHT } from "@/config";
import { draftBox } from "./create";
import type { Scale } from "./scale";
import { isoDay } from "./scale";

/** What an add row holds while it is being typed into. */
export interface AddDraft {
  typeId: string;
  summary: string;
}

/**
 * The sidebar side of an add row: a type and a summary, and Enter makes the
 * ticket under the group the row sits in. Escape clears what was typed.
 */
export function AddSidebarRow({
  label,
  types,
  draft,
  onChange,
  onSubmit,
  pending,
  error,
}: {
  label: string;
  types: IssueType[];
  draft: AddDraft;
  onChange: (draft: AddDraft) => void;
  onSubmit: () => void;
  pending: boolean;
  error?: string;
}) {
  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter" && draft.summary.trim()) {
      event.preventDefault();
      onSubmit();
    }
    if (event.key === "Escape") onChange({ ...draft, summary: "" });
  }

  return (
    <div
      className="flex items-center gap-1.5 border-b border-border/60 px-3 text-sm last:border-b-0"
      style={{ height: PLAN_ROW_HEIGHT, paddingLeft: PLAN_INDENT_PER_LEVEL }}
      data-plan-add={label}
      title={error}
    >
      <SelectInput
        aria-label={`Type of the issue to add to ${label}`}
        value={draft.typeId}
        onChange={(event) => onChange({ ...draft, typeId: event.target.value })}
        controlSize="sm"
        className="shrink-0 text-ink-muted"
        data-plan-add-type
      >
        {types.map((type) => (
          <option key={type.id} value={type.id}>
            {type.name}
          </option>
        ))}
      </SelectInput>
      <Input
        aria-label={`Add an issue to ${label}`}
        value={draft.summary}
        placeholder={pending ? "Adding..." : "Add an issue"}
        disabled={pending}
        onChange={(event) => onChange({ ...draft, summary: event.target.value })}
        onKeyDown={onKeyDown}
        controlSize="sm"
        invalid={Boolean(error)}
        className="min-w-0 flex-1 border-dashed bg-transparent"
      />
    </div>
  );
}

/** The days a drag to create has covered so far, drawn where the bar will be. */
export interface Ghost {
  rowKey: string;
  start: Date;
  due: Date;
}

/**
 * The calendar side of an add row: empty space a drag can be started on, the
 * ghost of the bar being dragged, and the summary box once the drag ends.
 */
export function AddCalendarRow({
  rowKey,
  label,
  top,
  scale,
  ghost,
  draft,
  onPointerDown,
  onDraftChange,
  onDraftSubmit,
  onDraftCancel,
}: {
  rowKey: string;
  label: string;
  top: number;
  scale: Scale;
  ghost: Ghost | null;
  draft: { start: Date; due: Date; summary: string } | null;
  onPointerDown: (event: ReactPointerEvent<HTMLElement>) => void;
  onDraftChange: (summary: string) => void;
  onDraftSubmit: () => void;
  onDraftCancel: () => void;
}) {
  const ghostBox = ghost && ghost.rowKey === rowKey ? { left: scale.x(ghost.start), width: scale.spanWidth(ghost.start, ghost.due) } : null;
  const box = draft ? draftBox(scale, draft.start, draft.due, scale.width) : null;

  return (
    <div
      className="absolute right-0 left-0 cursor-crosshair border-b border-border/60 touch-none select-none"
      style={{ top, height: PLAN_ROW_HEIGHT }}
      data-plan-add-row={label}
      onPointerDown={draft ? undefined : onPointerDown}
    >
      {ghostBox && !draft && (
        <span
          aria-hidden="true"
          data-plan-ghost
          data-plan-start={isoDay(ghost!.start)}
          data-plan-due={isoDay(ghost!.due)}
          className="absolute top-1/2 h-4 -translate-y-1/2 rounded border border-dashed border-accent bg-accent-subtle/60"
          style={{ left: ghostBox.left, width: ghostBox.width }}
        />
      )}
      {draft && box && (
        <DraftInput
          left={box.left}
          width={box.width}
          summary={draft.summary}
          range={`${isoDay(draft.start)} to ${isoDay(draft.due)}`}
          onChange={onDraftChange}
          onSubmit={onDraftSubmit}
          onCancel={onDraftCancel}
        />
      )}
    </div>
  );
}

/** The summary box a drag leaves behind: Enter makes the ticket, Escape or leaving it empty lets it go. */
function DraftInput({
  left,
  width,
  summary,
  range,
  onChange,
  onSubmit,
  onCancel,
}: {
  left: number;
  width: number;
  summary: string;
  range: string;
  onChange: (summary: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
}) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    ref.current?.focus();
  }, []);

  return (
    <Input
      ref={ref}
      aria-label={`Summary of the issue to add, ${range}`}
      title={range}
      value={summary}
      placeholder="What needs doing, then Enter"
      onChange={(event) => onChange(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === "Enter" && summary.trim()) {
          event.preventDefault();
          onSubmit();
        }
        if (event.key === "Escape") onCancel();
      }}
      onBlur={() => {
        if (!summary.trim()) onCancel();
      }}
      onPointerDown={(event) => event.stopPropagation()}
      controlSize="sm"
      className="absolute top-1/2 -translate-y-1/2 border-accent shadow-1"
      style={{ left, width }}
      data-plan-draft-summary
    />
  );
}
