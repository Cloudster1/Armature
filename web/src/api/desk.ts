import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { meQueryKey } from "./auth";
import { issuesQueryKey, type Comment, type Doc, type Issue, type Priority } from "./issues";
import type { Watcher } from "./watchers";

/**
 * The service desk: what customers raise through the portal, and the clocks
 * that say how long the desk has to answer.
 */
export interface RequestType {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  description?: string;
  issueTypeId: string;
  issueTypeName: string;
  priority: Priority;
  position: number;
  /** The heading it is offered under; empty is the general list. */
  category: string;
  /** What a requester starts their details from. */
  detailsTemplate: string;
  /** The team a request through it lands with; absent leaves it with the project. */
  teamId?: string;
  teamName?: string;
}

export type Metric = "first_response" | "resolution";

export interface Policy {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  metric: Metric;
  /** Minutes, keyed by priority. A priority with no goal is not measured. */
  goals: Partial<Record<Priority, number>>;
  pauseStatusIds: string[];
  pauseStatuses: string[];
  /** The goal counts only the project's business hours. */
  useCalendar: boolean;
}

export interface Timer {
  id: string;
  issueId: string;
  policyId: string;
  policyName: string;
  metric: Metric;
  goalMinutes: number;
  startedAt: string;
  runningSince?: string;
  completedAt?: string;
  breachedAt?: string;
  /** The goal counts only the desk's open hours. */
  businessHours?: boolean;
  elapsedSeconds: number;
  remainingSeconds: number;
  breached: boolean;
  paused: boolean;
}

export interface QueueRow {
  issue: Issue;
  timers: Timer[];
  requestTypeName?: string;
  /** What the customer said of it once resolved, 1 to 5. */
  csat?: number;
}

export type QueueFilter = "open" | "unassigned" | "mine" | "breached" | "all";

export interface Desk {
  projectId: string;
  projectKey: string;
  name: string;
  description?: string;
  requestTypes: RequestType[];
  /** Whether answering the desk's mails adds the words to the request. */
  repliesByMail: boolean;
}

export interface Request {
  issue: Issue;
  comments: Comment[];
  requestTypeName?: string;
}

export const deskQueryKey = ["desk"] as const;

export function useRequestTypes(projectKey: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "request-types", projectKey],
    queryFn: () => request<{ requestTypes: RequestType[] }>(`/projects/${projectKey}/request-types`),
    enabled: Boolean(projectKey),
  });
}

export function usePolicies(projectKey: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "policies", projectKey],
    queryFn: () => request<{ policies: Policy[] }>(`/projects/${projectKey}/sla-policies`),
    enabled: Boolean(projectKey),
  });
}

export function useQueue(projectKey: string, filter: QueueFilter) {
  return useQuery({
    queryKey: [...deskQueryKey, "queue", projectKey, filter],
    queryFn: () => request<{ rows: QueueRow[] }>(`/projects/${projectKey}/queue?filter=${filter}`),
    enabled: Boolean(projectKey),
    // A queue is a page people leave open; the clocks on it should move.
    refetchInterval: 30_000,
  });
}

export function useTimers(issueKey: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "timers", issueKey],
    queryFn: () => request<{ timers: Timer[] }>(`/issues/${issueKey}/timers`),
    enabled: Boolean(issueKey),
    refetchInterval: 30_000,
  });
}

function useDeskMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: deskQueryKey });
      queryClient.invalidateQueries({ queryKey: issuesQueryKey });
    },
  });
}

export interface RequestTypeInput {
  name?: string;
  description?: string;
  issueTypeId?: string;
  priority?: Priority;
  category?: string;
  detailsTemplate?: string;
  teamId?: string;
  /** Takes the team away, which leaving teamId out cannot say. */
  clearTeam?: boolean;
}

export function useCreateRequestType(projectKey: string) {
  return useDeskMutation((input: RequestTypeInput & { name: string }) =>
    request<{ requestType: RequestType }>(`/projects/${projectKey}/request-types`, { method: "POST", body: input }),
  );
}

export function useUpdateRequestType() {
  return useDeskMutation(({ id, ...input }: RequestTypeInput & { id: string }) =>
    request<{ requestType: RequestType }>(`/request-types/${id}`, { method: "PATCH", body: input }),
  );
}

export function useDeleteRequestType() {
  return useDeskMutation((id: string) => request<void>(`/request-types/${id}`, { method: "DELETE" }));
}

export function useUpdatePolicy() {
  return useDeskMutation(({ id, goals, useCalendar }: { id: string; goals?: Partial<Record<Priority, number>>; useCalendar?: boolean }) =>
    request<{ policy: Policy }>(`/sla-policies/${id}`, { method: "PATCH", body: { goals, useCalendar } }),
  );
}

// The desk's extras: articles, canned responses, ratings, business hours.

export interface Article {
  id: string;
  projectId: string;
  projectKey: string;
  title: string;
  body: string;
  published: boolean;
  authorName?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ArticleInput {
  title?: string;
  body?: string;
  published?: boolean;
}

export function useArticles(projectKey: string) {
  return useQuery({ queryKey: [...deskQueryKey, "articles", projectKey], queryFn: () => request<{ articles: Article[] }>(`/projects/${projectKey}/articles`), enabled: Boolean(projectKey) });
}

/** What a customer finds before raising a request. */
export function usePortalArticles(projectKey: string | undefined, query: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "portal-articles", projectKey, query],
    queryFn: () => request<{ articles: Article[] }>(`/portal/desks/${projectKey}/articles?q=${encodeURIComponent(query)}`),
    enabled: Boolean(projectKey),
  });
}

export function usePortalArticle(id: string) {
  return useQuery({ queryKey: [...deskQueryKey, "portal-article", id], queryFn: () => request<{ article: Article }>(`/portal/articles/${id}`), enabled: Boolean(id) });
}

export function useCreateArticle(projectKey: string) {
  return useDeskMutation((input: ArticleInput) => request<{ article: Article }>(`/projects/${projectKey}/articles`, { method: "POST", body: input }));
}

export function useUpdateArticle() {
  return useDeskMutation(({ id, ...input }: ArticleInput & { id: string }) => request<{ article: Article }>(`/articles/${id}`, { method: "PATCH", body: input }));
}

export function useDeleteArticle() {
  return useDeskMutation((id: string) => request<void>(`/articles/${id}`, { method: "DELETE" }));
}

export interface CannedResponse {
  id: string;
  projectKey: string;
  name: string;
  body: string;
}

export function useCannedResponses(projectKey: string) {
  return useQuery({ queryKey: [...deskQueryKey, "canned", projectKey], queryFn: () => request<{ responses: CannedResponse[] }>(`/projects/${projectKey}/canned-responses`), enabled: Boolean(projectKey) });
}

export function useCreateCanned(projectKey: string) {
  return useDeskMutation((input: { name: string; body: string }) => request<{ response: CannedResponse }>(`/projects/${projectKey}/canned-responses`, { method: "POST", body: input }));
}

export function useUpdateCanned() {
  return useDeskMutation(({ id, ...input }: { id: string; name?: string; body?: string }) => request<{ response: CannedResponse }>(`/canned-responses/${id}`, { method: "PATCH", body: input }));
}

export function useDeleteCanned() {
  return useDeskMutation((id: string) => request<void>(`/canned-responses/${id}`, { method: "DELETE" }));
}

/** A canned response with its placeholders filled in for one request. */
export function useRenderCanned() {
  return useMutation({ mutationFn: (input: { id: string; issueKey: string }) => request<{ text: string }>(`/canned-responses/${input.id}/render`, { method: "POST", body: { issueKey: input.issueKey } }) });
}

export interface Rating {
  issueKey: string;
  score?: number;
  comment?: string;
  sentAt: string;
  ratedAt?: string;
}

export interface RatingPage {
  issueKey: string;
  summary: string;
  orgName: string;
  rated: boolean;
}

export function useRatingPage(token: string) {
  return useQuery({ queryKey: ["rating", token], queryFn: () => request<{ rating: RatingPage }>(`/csat/${encodeURIComponent(token)}`), retry: false });
}

export function useRate(token: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { score: number; comment?: string }) => request<{ rating: Rating }>(`/csat/${encodeURIComponent(token)}`, { method: "POST", body: input }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["rating", token] }),
  });
}

export function useIssueRating(issueKey: string, enabled: boolean) {
  return useQuery({ queryKey: [...deskQueryKey, "rating", issueKey], queryFn: () => request<{ rating: Rating }>(`/issues/${issueKey}/csat`), enabled, retry: false });
}

/** When the desk is open: hours per weekday as from/to, days off, the zone. */
export interface BusinessCalendar {
  timezone: string;
  hours: Record<string, Array<{ from: string; to: string }>>;
  holidays: string[];
}

export function useBusinessCalendar(projectKey: string) {
  return useQuery({ queryKey: [...deskQueryKey, "calendar", projectKey], queryFn: () => request<{ calendar: BusinessCalendar }>(`/projects/${projectKey}/business-calendar`), enabled: Boolean(projectKey), retry: false });
}

export function useSaveBusinessCalendar(projectKey: string) {
  return useDeskMutation((input: BusinessCalendar) => request<{ calendar: BusinessCalendar }>(`/projects/${projectKey}/business-calendar`, { method: "PUT", body: input }));
}

/** An internal note: a comment the customer is never shown. */
export function useAddNote() {
  return useDeskMutation(({ key, body }: { key: string; body: Doc }) =>
    request<{ comment: Comment }>(`/issues/${key}/notes`, { method: "POST", body: { body } }),
  );
}

// The portal.

export function useDesks() {
  return useQuery({
    queryKey: [...deskQueryKey, "portal", "desks"],
    queryFn: () => request<{ desks: Desk[] }>("/portal/desks"),
  });
}

export function useMyRequests() {
  return useQuery({
    queryKey: [...deskQueryKey, "portal", "requests"],
    queryFn: () => request<{ requests: Issue[] }>("/portal/requests"),
  });
}

export function useRequest(key: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "portal", "request", key],
    queryFn: () => request<Request>(`/portal/requests/${key}`),
    enabled: Boolean(key),
  });
}

export function useRaiseRequest() {
  return useDeskMutation((input: { requestTypeId: string; summary: string; description?: string }) =>
    request<{ request: Issue }>("/portal/requests", { method: "POST", body: input }),
  );
}

export function useReply() {
  return useDeskMutation(({ key, text }: { key: string; text: string }) =>
    request<{ comment: Comment }>(`/portal/requests/${key}/replies`, { method: "POST", body: { text } }),
  );
}

// A request's followers, from the requester's side.

export function useRequestWatchers(key: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "portal", "watchers", key],
    queryFn: () => request<{ watchers: Watcher[] }>(`/portal/requests/${key}/watchers`),
    enabled: Boolean(key),
  });
}

export function useFollow() {
  return useDeskMutation(({ key, email }: { key: string; email: string }) =>
    request<{ watcher: Watcher }>(`/portal/requests/${key}/watchers`, { method: "POST", body: { email } }),
  );
}

export function useUnfollow() {
  return useDeskMutation(({ key, userId }: { key: string; userId: string }) =>
    request<void>(`/portal/requests/${key}/watchers/${userId}`, { method: "DELETE" }),
  );
}

// The door: a desk named by its organization, for people without an account.

export interface DeskEntry {
  name: string;
  slug: string;
  /** The desks that let people in without a code. */
  open: OpenDoor[];
}

export interface OpenDoor {
  key: string;
  name: string;
}

export function useDeskEntry(slug: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "entry", slug],
    queryFn: () => request<DeskEntry>(`/desk/${slug}`),
    enabled: Boolean(slug),
    retry: false,
  });
}

export function useRequestCode() {
  return useMutation({
    mutationFn: ({ slug, email }: { slug: string; email: string }) =>
      request<{ expiresInSeconds: number }>(`/desk/${slug}/codes`, { method: "POST", body: { email } }),
  });
}

export function useEnterWithCode() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ slug, email, code }: { slug: string; email: string; code: string }) =>
      request<{ principal: unknown }>(`/desk/${slug}/sessions`, { method: "POST", body: { email, code } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQueryKey }),
  });
}

/** Walks into a desk whose door is open: a name and an address, no code. */
export function useEnterOpenDoor() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ slug, desk, email, name }: { slug: string; desk: string; email: string; name: string }) =>
      request<{ principal: unknown }>(`/desk/${slug}/sessions`, { method: "POST", body: { email, name, desk } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQueryKey }),
  });
}

/** A duration in the words a person uses for it: 3h 20m, 2d 4h, 45m. */
export function humanDuration(seconds: number): string {
  const s = Math.abs(Math.round(seconds));
  const days = Math.floor(s / 86_400);
  const hours = Math.floor((s % 86_400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}

/** What a metric is called in a sentence. */
export function metricName(metric: Metric): string {
  return metric === "first_response" ? "First response" : "Resolution";
}
