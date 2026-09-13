import { useState } from "react";
import { useConfirmProposal, type AssistantAnswer, type AssistantProposal } from "@/api/assistant";
import { Button } from "@/components/ui";

/**
 * What the model answered, and what it proposes to change. A proposal happens
 * when the reader presses Confirm, never because the model asked for it.
 */
export function AskAnswer({
  pending,
  error,
  answer,
  proposals,
  onGo,
}: {
  pending: boolean;
  error: unknown;
  answer: AssistantAnswer | null;
  proposals: AssistantProposal[];
  onGo: (answer: AssistantAnswer) => void;
}) {
  const confirmProposal = useConfirmProposal();
  const [done, setDone] = useState<string[]>([]);

  return (
    <div className="border-b border-border px-3 py-3 text-sm" data-assistant-answer>
      {pending && <p className="text-ink-subtle">Asking...</p>}
      {error ? <p className="text-danger">{(error as Error).message}</p> : null}
      {answer && (
        <>
          <p className="text-ink">{answer.text}</p>
          {answer.to && (
            <Button size="sm" className="mt-2" onClick={() => onGo(answer)} data-action="go-there">
              Go there
            </Button>
          )}
          {proposals.map((proposal) => (
            <div key={proposal.tool} className="mt-2 flex items-center gap-2" data-proposal={proposal.tool}>
              <span className="min-w-0 flex-1 text-ink-subtle">{proposal.says}</span>
              {done.includes(proposal.tool) ? (
                <span className="text-ink-subtle">Done</span>
              ) : (
                <Button
                  size="sm"
                  variant="secondary"
                  loading={confirmProposal.isPending}
                  onClick={() => confirmProposal.mutate(proposal, { onSuccess: () => setDone((was) => [...was, proposal.tool]) })}
                  data-action="confirm-proposal"
                >
                  Confirm
                </Button>
              )}
            </div>
          ))}
        </>
      )}
    </div>
  );
}
