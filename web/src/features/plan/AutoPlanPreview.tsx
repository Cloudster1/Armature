import { useEffect } from "react";
import { Link } from "@tanstack/react-router";
import type { Alignment } from "@/api/plan";
import { Button, Card, Checkbox, ErrorBanner } from "@/components/ui";
import { PLAN_ALIGN_MAX_MOVES } from "@/config";
import { describeMove, summarise } from "./autoplan";
import { t } from "@/i18n";

/**
 * What the auto plan would move, before anything does. Apply saves every move
 * at once; Cancel and Escape drop them. The ghost bars on the timeline show the same.
 */
export function AutoPlanPreview({
  alignment,
  locale,
  applying,
  place,
  onPlace,
  reading = false,
  refusal,
  onApply,
  onCancel,
  onPreviewAgain,
}: {
  alignment: Alignment;
  locale: string;
  applying: boolean;
  /** Whether issues without dates are placed too, and asking again with the other answer. */
  place: boolean;
  onPlace: (place: boolean) => void;
  /** A proposal is being read again, so the one shown is about to change. */
  reading?: boolean;
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
    <Card className="mb-3 space-y-3 p-4" role="region" aria-label={t.plan.autoPlan.title} data-testid="auto-plan-preview">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-sm font-semibold text-ink">{t.plan.autoPlan.title}</h2>
        <p className="text-sm text-ink-muted" data-auto-plan-count={moves.length}>
          {summarise(alignment)}
        </p>
      </div>
      <Checkbox
        label={t.plan.autoPlan.place}
        checked={place}
        disabled={reading || applying}
        onChange={(event) => onPlace(event.target.checked)}
        data-auto-plan-place
      />

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

      {alignment.unplaced.length > 0 && (
        <ul className="space-y-1 text-sm" data-testid="auto-plan-unplaced">
          {alignment.unplaced.map((u) => (
            <li key={u.issueKey} className="flex items-baseline gap-2" data-auto-plan-unplaced={u.issueKey}>
              <span className="shrink-0 font-mono text-xs text-ink">{u.issueKey}</span>
              <span className="text-ink-muted">{u.reason}</span>
            </li>
          ))}
        </ul>
      )}

      {left.length > 0 && (
        <div>
          <p className="mb-1 text-xs font-medium text-ink-muted">{t.plan.autoPlan.disagree(left.length)}</p>
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

      {tooMany && <ErrorBanner>{t.plan.autoPlan.tooMany(moves.length, PLAN_ALIGN_MAX_MOVES)}</ErrorBanner>}
      {refusal && (
        <ErrorBanner>
          <span className="mr-2">{refusal}</span>
          <Button size="sm" variant="secondary" onClick={onPreviewAgain} data-action="auto-plan-again">
            {t.plan.autoPlan.previewAgain}
          </Button>
        </ErrorBanner>
      )}

      <div className="flex gap-2">
        {moves.length > 0 && (
          <Button onClick={onApply} disabled={applying || reading || tooMany || Boolean(refusal)} data-action="auto-plan-apply">
            {applying ? t.plan.autoPlan.applying : t.plan.autoPlan.apply}
          </Button>
        )}
        <Button variant="secondary" onClick={onCancel} data-action="auto-plan-cancel">
          {moves.length > 0 ? t.plan.autoPlan.cancel : t.plan.autoPlan.close}
        </Button>
      </div>
    </Card>
  );
}
