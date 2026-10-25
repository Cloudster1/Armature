import { useState } from "react";
import { ApiError } from "@/api/client";
import { type Transition, type TransitionNeed, useTransitionIssue, useTransitions } from "@/api/issues";
import { Button, Dialog, ErrorBanner, Field, Select } from "@/components/ui";
import { PersonPicker } from "@/features/people/PersonPicker";
import { TRANSITION_COMMENT_ROWS } from "@/config";
import { t } from "@/i18n";

const NEEDS: readonly TransitionNeed[] = ["comment", "resolution", "assignee"];
/** The code the server refuses a move with when a validator wants something. */
const VALIDATION_FAILED = "validation_failed";

/** The needs a refusal names, for a transition whose needs the list did not carry. */
export function refusedNeeds(error: unknown): TransitionNeed[] {
  if (!(error instanceof ApiError) || error.code !== VALIDATION_FAILED) return [];
  return NEEDS.filter((need) => need in error.fields);
}

interface Asking {
  transition: Transition;
  needs: TransitionNeed[];
}

// The transitions the workflow currently allows. The server decides what is
// offered, so a transition the user may not take never appears as a button.
export function TransitionBar({ issueKey }: { issueKey: string }) {
  const { data, isLoading } = useTransitions(issueKey);
  const transition = useTransitionIssue();
  const [asking, setAsking] = useState<Asking | null>(null);
  const transitions = data?.transitions ?? [];

  if (isLoading) return null;

  // A move whose rules want something asks for it first; one the list did not
  // know about asks once the server says what is missing.
  function take(chosen: Transition) {
    if (chosen.needs?.length) {
      setAsking({ transition: chosen, needs: chosen.needs });
      return;
    }
    transition.mutate(
      { key: issueKey, transitionId: chosen.id },
      {
        onError: (error) => {
          const needs = refusedNeeds(error);
          if (needs.length > 0) {
            transition.reset();
            setAsking({ transition: chosen, needs });
          }
        },
      },
    );
  }

  return (
    <div className="flex max-w-full min-w-0 flex-col items-end gap-2">
      {transitions.length === 0 ? (
        <p className="text-sm text-ink-subtle">{t.issueView.header.nowhereToGo}</p>
      ) : (
        <div className="flex flex-wrap justify-end gap-2">
          {transitions.map((each) => (
            <Button
              key={each.id}
              size="sm"
              variant="secondary"
              title={each.description || undefined}
              loading={transition.isPending && transition.variables?.transitionId === each.id}
              onClick={() => take(each)}
              data-transition={each.name}
            >
              {each.name}
            </Button>
          ))}
        </div>
      )}
      {transition.error && <ErrorBanner>{(transition.error as Error).message}</ErrorBanner>}
      {asking && <TransitionDialog issueKey={issueKey} asking={asking} onClose={() => setAsking(null)} />}
    </div>
  );
}

const resolutionChoices = () => {
  const words = t.issueView.transition.resolutions;
  return [words.done, words.wontDo, words.duplicate, words.cannotReproduce];
};

/** Asks for what a transition's rules want and sends it with the move. */
function TransitionDialog({ issueKey, asking, onClose }: { issueKey: string; asking: Asking; onClose: () => void }) {
  const transition = useTransitionIssue();
  const [comment, setComment] = useState("");
  const [resolution, setResolution] = useState("");
  const [assigneeId, setAssigneeId] = useState("");
  const wants = (need: TransitionNeed) => asking.needs.includes(need);
  const missing = (wants("comment") && !comment.trim()) || (wants("resolution") && !resolution) || (wants("assignee") && !assigneeId);
  const fields = transition.error instanceof ApiError ? transition.error.fields : {};

  function send() {
    transition.mutate(
      {
        key: issueKey,
        transitionId: asking.transition.id,
        comment: wants("comment") ? comment.trim() : undefined,
        resolution: wants("resolution") ? resolution : undefined,
        assigneeId: wants("assignee") ? assigneeId : undefined,
      },
      { onSuccess: onClose },
    );
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={asking.transition.name}
      description={t.issueView.transition.needs}
      size="sm"
      attrs={{ "data-transition-dialog": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t.issueView.transition.cancel}
          </Button>
          <Button loading={transition.isPending} disabled={missing} onClick={send} data-action="transition-go">
            {asking.transition.name}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {wants("resolution") && (
          <Select label={t.issueView.transition.resolution} value={resolution} onChange={(e) => setResolution(e.target.value)} error={fields.resolution}>
            <option value="">{t.issueView.transition.chooseResolution}</option>
            {resolutionChoices().map((choice) => (
              <option key={choice} value={choice}>
                {choice}
              </option>
            ))}
          </Select>
        )}
        {wants("assignee") && (
          <PersonPicker
            label={t.issueView.transition.assignee}
            staff
            none={t.issueView.transition.chooseAssignee}
            value={assigneeId}
            onChange={setAssigneeId}
            hint={fields.assignee}
          />
        )}
        {wants("comment") && (
          <Field
            label={t.issueView.transition.comment}
            rows={TRANSITION_COMMENT_ROWS}
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            error={fields.comment}
          />
        )}
        {transition.error && !fields.comment && !fields.resolution && !fields.assignee && <ErrorBanner>{(transition.error as Error).message}</ErrorBanner>}
      </div>
    </Dialog>
  );
}
