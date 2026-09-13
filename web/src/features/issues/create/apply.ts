import { request } from "@/api/client";
import type { Draft } from "./draft";

/** One answer the new issue did not end up carrying, and what the server said. */
export interface Leftover {
  what: string;
  why: string;
}

interface Step {
  what: string;
  run: () => Promise<unknown>;
}

// Each step stands alone, as in the CSV import: one refusal is reported and
// the others still happen, rather than an issue half made.
function steps(issueKey: string, draft: Draft): Step[] {
  const rest: Step[] = [];
  const patch = (what: string, body: unknown) => rest.push({ what, run: () => request(`/issues/${issueKey}`, { method: "PATCH", body }) });

  if (draft.reporterId) patch("Reporter", { reporterId: draft.reporterId });
  if (draft.timeEstimate.trim()) patch("Time estimate", { timeEstimateMinutes: Number(draft.timeEstimate) });
  if (draft.labels.length > 0) {
    rest.push({ what: "Labels", run: () => request(`/issues/${issueKey}/labels`, { method: "PUT", body: { labels: draft.labels } }) });
  }
  if (draft.fixVersionIds.length > 0 || draft.affectsVersionIds.length > 0) {
    const body = { fix: draft.fixVersionIds, affects: draft.affectsVersionIds };
    rest.push({ what: "Versions", run: () => request(`/issues/${issueKey}/versions`, { method: "PUT", body }) });
  }
  if (draft.milestoneId) {
    rest.push({ what: "Milestone", run: () => request(`/issues/${issueKey}/milestone`, { method: "PUT", body: { milestoneId: draft.milestoneId } }) });
  }
  for (const [fieldId, answer] of Object.entries(draft.values)) {
    if (answer.value === null || answer.value === false) continue;
    rest.push({ what: answer.name, run: () => request(`/issues/${issueKey}/fields/${fieldId}`, { method: "PUT", body: { value: answer.value } }) });
  }
  return rest;
}

/** Applies everything the create left over, and reports what would not stick. */
export async function applyRest(issueKey: string, draft: Draft): Promise<Leftover[]> {
  const left: Leftover[] = [];
  for (const step of steps(issueKey, draft)) {
    try {
      await step.run();
    } catch (err) {
      left.push({ what: step.what, why: err instanceof Error ? err.message : String(err) });
    }
  }
  return left;
}
