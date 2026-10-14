import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Issue, IssueType, Status } from "@/api/issues";
import { projectWorkflowsQuery, workflowQuery, type Assignment, type Workflow } from "@/api/workflows";

/**
 * The types an issue may become here: the server moves an issue to another
 * level only by refusing, so only its own level, subtask or not, is offered.
 */
export function typeChoices(types: IssueType[], issue: Pick<Issue, "type">): IssueType[] {
  return types.filter((t) => t.id !== issue.type.id && t.level === issue.type.level && t.isSubtask === issue.type.isSubtask);
}

/** The workflow a type follows in a project, as the project's table answers it. */
export function workflowFor(assignments: Assignment[], typeId: string): string | undefined {
  return assignments.find((a) => a.issueTypeId === typeId)?.workflowId;
}

/** Where an issue in this status lands under the workflow; null when it keeps the status. */
export function landingStatus(workflow: Workflow, status: Status): Status | null {
  if (workflow.steps.some((step) => step.status.id === status.id)) return null;
  return workflow.steps.find((step) => step.isInitial)?.status ?? null;
}

/**
 * Works out, as the server will, which status a type change moves the issue
 * to, or null when it stays put or the project names no workflow for the type.
 */
export function useTypeChange(issue: Pick<Issue, "projectKey" | "status">) {
  const queryClient = useQueryClient();
  const { projectKey, status } = issue;
  return useCallback(
    async (typeId: string): Promise<Status | null> => {
      const { assignments } = await queryClient.ensureQueryData(projectWorkflowsQuery(projectKey));
      const workflowId = workflowFor(assignments, typeId);
      if (!workflowId) return null;
      const { workflow } = await queryClient.ensureQueryData(workflowQuery(workflowId));
      return landingStatus(workflow, status);
    },
    [queryClient, projectKey, status],
  );
}
