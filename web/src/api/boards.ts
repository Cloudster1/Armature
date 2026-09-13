import type { LabelRef } from "./issues";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import type { Issue, ParentRef, Priority, Status, TypeRef, UserRef } from "./issues";

export type Grouping = "none" | "assignee" | "priority" | "type";

/**
 * How a board decides which of its work to show. A scrum board draws the sprint
 * that is running in its stream and nothing else; a kanban board draws
 * everything in scope and leans on swimlane limits instead.
 */
export type BoardType = "scrum" | "kanban";

/** The running sprint a scrum board is showing. */
export interface SprintRef {
  id: string;
  name: string;
  goal?: string;
  endsOn?: string;
}

export interface Card {
  labels: LabelRef[];
  id: string;
  key: string;
  summary: string;
  type: TypeRef;
  parent?: ParentRef;
  status: Status;
  priority: Priority;
  assignee?: UserRef;
  parentKey?: string;
  rank: string;
  group?: string;
  updatedAt: string;
}

export interface Swimlane {
  id: string;
  name: string;
  position: number;
  wipLimit: number;
  /** The ticket states this swimlane is made of. A card is here if its status is. */
  statuses: Status[];
  cards: Card[];
}

export interface Board {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  description?: string;
  type: BoardType;
  groupBy: Grouping;
  /** What a scrum board is showing; absent when nothing is running, or on kanban. */
  sprint?: SprintRef;
  swimlanes: Swimlane[];
  /** Cards whose status no swimlane claims, surfaced rather than hidden. */
  unmapped: Card[];
  /** The team this board draws from; absent is a board over the whole project. */
  teamId?: string;
  teamName?: string;
  createdAt: string;
  updatedAt: string;
}

/** A board without its cards, for choosing between the boards a project has. */
export interface BoardSummary {
  id: string;
  name: string;
  description?: string;
  type: BoardType;
  teamId?: string;
  teamName?: string;
  swimlaneCount: number;
}

export const boardQueryKey = ["board"] as const;

export function useBoard(projectKey: string) {
  return useQuery({
    queryKey: [...boardQueryKey, projectKey],
    queryFn: () => request<{ board: Board }>(`/projects/${projectKey}/board`),
    enabled: Boolean(projectKey),
  });
}

/** The boards a project has. Most have one; a project with teams has several. */
export function useBoards(projectKey: string) {
  return useQuery({
    queryKey: [...boardQueryKey, "list", projectKey],
    queryFn: () => request<{ boards: BoardSummary[] }>(`/projects/${projectKey}/boards`),
    enabled: Boolean(projectKey),
  });
}

/** One board by id, which is how a project with several is read. */
export function useBoardById(id: string) {
  return useQuery({
    queryKey: [...boardQueryKey, "detail", id],
    queryFn: () => request<{ board: Board }>(`/boards/${id}`),
    enabled: Boolean(id),
  });
}

/** A sprint's own board: its stream's board showing only what is committed to it. */
export function useSprintBoard(sprintId: string) {
  return useQuery({
    queryKey: [...boardQueryKey, "sprint", sprintId],
    queryFn: () => request<{ board: Board }>(`/sprints/${sprintId}/board`),
    enabled: Boolean(sprintId),
  });
}

/** The work in a board's scope that is not committed to a sprint. */
export function useBacklog(boardId: string) {
  return useQuery({
    queryKey: [...boardQueryKey, "backlog", boardId],
    queryFn: () => request<{ cards: Card[] }>(`/boards/${boardId}/backlog`),
    enabled: Boolean(boardId),
  });
}

export interface BoardInput {
  name?: string;
  description?: string;
  /** Left out, a new board takes after the project's first one. */
  type?: BoardType;
  groupBy?: Grouping;
  teamId?: string | null;
}

export function useCreateBoard(projectKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: BoardInput) =>
      request<{ board: Board }>(`/projects/${projectKey}/boards`, { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useUpdateBoard() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...input }: BoardInput & { id: string }) =>
      request<{ board: Board }>(`/boards/${id}`, { method: "PATCH", body: input }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export function useDeleteBoard() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => request<void>(`/boards/${id}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

export interface MoveInput {
  projectKey: string;
  issueKey: string;
  swimlaneId: string;
  afterKey?: string;
  beforeKey?: string;
}

export interface MoveResult {
  issue: Issue;
  /** The workflow transition the drag turned into, absent for a reorder. */
  transition?: string;
  rank: string;
}

export function useMoveCard() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectKey, ...body }: MoveInput) =>
      request<MoveResult>(`/projects/${projectKey}/board/move`, { method: "POST", body }),
    // A move can change status, assignee and resolution at once, so everything
    // about the board and the issue is stale afterwards.
    onSettled: () => queryClient.invalidateQueries(),
  });
}

export function useAddSwimlane() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      projectKey,
      ...body
    }: {
      projectKey: string;
      name: string;
      statusIds?: string[];
      wipLimit?: number;
    }) => request<{ swimlane: Swimlane }>(`/projects/${projectKey}/board/swimlanes`, { method: "POST", body }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: boardQueryKey }),
  });
}

export function useUpdateSwimlane() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      projectKey,
      swimlaneId,
      ...body
    }: {
      projectKey: string;
      swimlaneId: string;
      name?: string;
      wipLimit?: number;
      statusIds?: string[];
    }) =>
      request<void>(`/projects/${projectKey}/board/swimlanes/${swimlaneId}`, {
        method: "PATCH",
        body,
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: boardQueryKey }),
  });
}

export function useDeleteSwimlane() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectKey, swimlaneId }: { projectKey: string; swimlaneId: string }) =>
      request<void>(`/projects/${projectKey}/board/swimlanes/${swimlaneId}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: boardQueryKey }),
  });
}

export function useReorderSwimlanes() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectKey, order }: { projectKey: string; order: string[] }) =>
      request<void>(`/projects/${projectKey}/board/swimlanes`, { method: "PUT", body: { order } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: boardQueryKey }),
  });
}

export function useSetGrouping() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ projectKey, groupBy }: { projectKey: string; groupBy: Grouping }) =>
      request<void>(`/projects/${projectKey}/board`, { method: "PATCH", body: { groupBy } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: boardQueryKey }),
  });
}

/**
 * Splits a swimlane's cards into the rows the board groups by, preserving rank
 * order within each. Returns a single unnamed row when there is no grouping,
 * so the renderer has one shape to deal with either way.
 */
export function groupCards(cards: Card[], groupBy: Grouping): Array<{ name: string; cards: Card[] }> {
  if (groupBy === "none") return [{ name: "", cards }];

  const rows = new Map<string, Card[]>();
  for (const card of cards) {
    const name = card.group ?? "";
    const existing = rows.get(name);
    if (existing) existing.push(card);
    else rows.set(name, [card]);
  }
  return [...rows.entries()]
    .map(([name, groupedCards]) => ({ name, cards: groupedCards }))
    .sort((a, b) => a.name.localeCompare(b.name));
}
