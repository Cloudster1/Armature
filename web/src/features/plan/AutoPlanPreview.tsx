import { useEffect } from "react";
import { Link } from "@tanstack/react-router";
import type { Alignment } from "@/api/plan";
import { Button, Card, ErrorBanner } from "@/components/ui";
import { PLAN_ALIGN_MAX_MOVES } from "@/config";
import { describeMove } from "./autoplan";

/**
 * What the auto plan would move, before anything does. Apply saves every move
 * at once; Cancel and Escape drop them. The ghost bars on the timeline show the same.
 */
export function AutoPlanPreview({
  alignment,
  locale,
  applying,
  refusal,
  onApply,
  onCancel,
  onPreviewAgain,
}: {
  alignment: Alignment;
  locale: string;
  applying: boolean;
  /** Why the moves were not saved, such as an issue moved by someone else meanwhile. */
  refusal?: string;
  onApply: () => void;
  onCancel: () => void;
  onPreviewAgain: () => void;
}) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onCancel();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel]);

  const { moves } = alignment;
  const tooMany = moves.length > PLAN_ALIGN_MAX_MOVES;
  const left = alignment.warnings.filter((w) => w.issueKey);

  return (
    <Card className="mb-3 space-y-3 p-4" role="region" aria-label="Auto plan" data-testid="auto-plan-preview">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-sm font-semibold text-ink">Auto plan</h2>
        <p className="text-sm text-ink-muted" data-auto-plan-count={moves.length}>
          {moves.length === 0
            ? "Every issue already starts after the issues blocking it."
            : `${moves.length} issue${moves.length === 1 ? "" : "s"} would move to start after the issues blocking them. Nothing is saved until you apply.`}
        </p>
      </div>

      {moves.length > 0 && (
        <ul className="max-h-64 space-y-1 overflow-y-auto text-sm" data-testid="auto-plan-moves">
          {moves.map((move) => (
            <li key={move.issueKey} className="flex items-baseline gap-2" data-auto-plan-move={move.issueKey}>
              <Link to="/issues/$issueKey" params={{ issueKey: move.issueKey }} className="shrink-0 font-mono text-xs text-accent hover:underline">
                {move.issueKey}
              </Link>
              <span className="text-ink-muted">{describeMove(move, locale)}</span>
            </li>
          ))}
        </ul>
      )}

      {left.length > 0 && (
        <div>
          <p className="mb-1 text-xs font-medium text-ink-muted">What the plan would still disagree on ({left.length})</p>
          <ul className="space-y-1 text-sm" data-testid="auto-plan-warnings">
            {left.map((w) => (
              <li key={`${w.issueKey}-${w.kind}-${w.message}`} className="flex items-baseline gap-2" data-auto-plan-warning-for={w.issueKey}>
                <span className="shrink-0 font-mono text-xs text-ink">{w.issueKey}</span>
                <span className="text-ink-muted">{w.message}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {tooMany && (
        <ErrorBanner>{`The auto plan would move ${moves.length} issues, and at most ${PLAN_ALIGN_MAX_MOVES} move at once. Narrow the plan with a query and try again.`}</ErrorBanner>
      )}
      {refusal && (
        <ErrorBanner>
          <span className="mr-2">{refusal}</span>
          <Button size="sm" variant="secondary" onClick={onPreviewAgain} data-action="auto-plan-again">
            Preview again
          </Button>
        </ErrorBanner>
      )}

      <div className="flex gap-2">
        {moves.length > 0 && (
          <Button onClick={onApply} disabled={applying || tooMany || Boolean(refusal)} data-action="auto-plan-apply">
            {applying ? "Applying..." : "Apply"}
          </Button>
        )}
        <Button variant="secondary" onClick={onCancel} data-action="auto-plan-cancel">
          {moves.length > 0 ? "Cancel" : "Close"}
        </Button>
      </div>
    </Card>
  );
}
