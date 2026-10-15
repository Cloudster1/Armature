import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { QUEUE_PAGE_SIZE } from "@/config";
import { request } from "./client";
import { meQueryKey } from "./auth";
import { issuesQueryKey, type Comment, type Doc, type Issue, type Priority, type StatusCategory } from "./issues";

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

/** One page of a queue, and how many requests the whole queue holds. */
export interface QueuePage {
  rows: QueueRow[];
  total: number;
  limit: number;
  offset: number;
}

export interface Desk {
  projectId: string;
  projectKey: string;
  name: string;
  description?: string;
  requestTypes: RequestType[];
  /** Whether answering the desk's mails adds the words to the request. */
  repliesByMail: boolean;
  /** Where a new request may be mailed; absent when this desk raises nothing from mail. */
  mailAddress?: string;
  /** The customer's company here, which a request can be shared with. */
  company?: { id: string; name: string };
}

/** A desk's customers who read the requests a colleague shared with them. */
export interface Company {
  id: string;
  projectId: string;
  projectKey: string;
  name: string;
  /** Where a customer joins from on their first request at the desk. */
  domains: string[];
  members: CompanyMember[];
  createdAt: string;
}

export interface CompanyMember {
  userId: string;
  name: string;
  email: string;
  addedAt: string;
}

/** Somebody on a request as the portal names them. */
export interface PortalPerson {
  id: string;
  name: string;
  /** Where the picture is served from; absent means initials. */
  avatarUrl?: string;
}

/** A request as its customer reads it. */
export interface PortalRequest {
  key: string;
  summary: string;
  description?: Doc;
  status: { name: string; category: StatusCategory };
  requestTypeName?: string;
  /** Who the request type hands it to. */
  team?: { id: string; name: string };
  company?: { id: string; name: string };
  sharedWithCompany: boolean;
  assignee?: PortalPerson;
  reporter?: PortalPerson;
  updatedAt: string;
}

/** One reply in a request's public conversation. */
export interface PortalComment {
  id: string;
  author?: PortalPerson;
  body: Doc;
  createdAt: string;
}

/** Somebody told about a request; the address only for the people the reporter added. */
export interface PortalFollower {
  userId: string;
  name: string;
  email?: string;
}

/** The portal's two lists: what the customer is on, and what colleagues shared. */
export interface MyRequests {
  requests: PortalRequest[];
  shared: PortalRequest[];
}

export interface Request {
  request: PortalRequest;
  comments: PortalComment[];
  /** The reader sees it only as a colleague, who is not shown its followers. */
  throughCompany?: boolean;
}

export const deskQueryKey = ["desk"] as const;

export function useRequestTypes(projectKey: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "request-types", projectKey],
    queryFn: () => request<{ requestTypes: RequestType[] }>(`/projects/${projectKey}/request-types`),
    enabled: Boolean(projectKey),
  });
}

/** The desk's own address, empty when the installation reads no mailbox. */
export function useDeskMailAddress(projectKey: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "mail-address", projectKey],
    queryFn: () => request<{ address: string }>(`/projects/${projectKey}/mail-address`),
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

export function useQueue(projectKey: string, filter: QueueFilter, offset = 0, company = "") {
  return useQuery({
    queryKey: [...deskQueryKey, "queue", projectKey, filter, offset, company],
    queryFn: () =>
      request<QueuePage>(`/projects/${projectKey}/queue?filter=${filter}&limit=${QUEUE_PAGE_SIZE}&offset=${offset}${company ? `&company=${company}` : ""}`),
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
  /** The desk's name, which is who the customer dealt with. */
  desk: string;
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
  /** The desk's own days off, closed beside the holiday calendar's. */
  holidays: string[];
  /** The holiday calendar whose days close the desk too; its name is only read. */
  holidayCalendarId: string | null;
  holidayCalendarName: string | null;
}

export function useBusinessCalendar(projectKey: string) {
  return useQuery({ queryKey: [...deskQueryKey, "calendar", projectKey], queryFn: () => request<{ calendar: BusinessCalendar }>(`/projects/${projectKey}/business-calendar`), enabled: Boolean(projectKey), retry: false });
}

export function useSaveBusinessCalendar(projectKey: string) {
  return useDeskMutation((input: BusinessCalendar) => request<{ calendar: BusinessCalendar }>(`/projects/${projectKey}/business-calendar`, { method: "PUT", body: input }));
}

export function useCompanies(projectKey: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "companies", projectKey],
    queryFn: () => request<{ companies: Company[] }>(`/projects/${projectKey}/companies`),
    enabled: Boolean(projectKey),
  });
}

export function useCreateCompany(projectKey: string) {
  return useDeskMutation((input: { name: string; domains?: string[] }) =>
    request<{ company: Company }>(`/projects/${projectKey}/companies`, { method: "POST", body: input }),
  );
}

export function useUpdateCompany() {
  return useDeskMutation(({ id, ...input }: { id: string; name?: string; domains?: string[] }) =>
    request<{ company: Company }>(`/companies/${id}`, { method: "PATCH", body: input }),
  );
}

export function useDeleteCompany() {
  return useDeskMutation((id: string) => request<void>(`/companies/${id}`, { method: "DELETE" }));
}

export function useAddCompanyMember() {
  return useDeskMutation(({ id, email }: { id: string; email: string }) =>
    request<{ company: Company }>(`/companies/${id}/members`, { method: "POST", body: { email } }),
  );
}

export function useRemoveCompanyMember() {
  return useDeskMutation(({ id, userId }: { id: string; userId: string }) =>
    request<void>(`/companies/${id}/members/${userId}`, { method: "DELETE" }),
  );
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
    queryFn: () => request<MyRequests>("/portal/requests"),
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
  return useDeskMutation((input: { requestTypeId: string; summary: string; description?: string; shareWithCompany?: boolean }) =>
    request<{ request: PortalRequest }>("/portal/requests", { method: "POST", body: input }),
  );
}

export function useReply() {
  return useDeskMutation(({ key, text }: { key: string; text: string }) =>
    request<{ comment: PortalComment }>(`/portal/requests/${key}/replies`, { method: "POST", body: { text } }),
  );
}

// A request's followers, from the requester's side.

export function useRequestWatchers(key: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "portal", "watchers", key],
    queryFn: () => request<{ watchers: PortalFollower[] }>(`/portal/requests/${key}/watchers`),
    enabled: Boolean(key),
  });
}

export function useFollow() {
  return useDeskMutation(({ key, email }: { key: string; email: string }) =>
    request<{ watcher: PortalFollower }>(`/portal/requests/${key}/watchers`, { method: "POST", body: { email } }),
  );
}

export function useUnfollow() {
  return useDeskMutation(({ key, userId }: { key: string; userId: string }) =>
    request<void>(`/portal/requests/${key}/watchers/${userId}`, { method: "DELETE" }),
  );
}

// The door for people without an account: the installation's, or a desk's
// when its key is in the address.

export interface PortalDoor {
  installation: string;
  /** The desk the address names, if it names one. */
  desk?: DoorDesk;
}

export interface DoorDesk {
  key: string;
  name: string;
  /** Whether it lets people in without a code. */
  open: boolean;
}

/** The query that names a desk's door, or nothing for the installation's. */
function deskQuery(desk?: string): string {
  return desk ? `?${new URLSearchParams({ desk })}` : "";
}

export function useDeskEntry(desk?: string) {
  return useQuery({
    queryKey: [...deskQueryKey, "entry", desk ?? ""],
    queryFn: () => request<PortalDoor>(`/desk${deskQuery(desk)}`),
    retry: false,
  });
}

export function useRequestCode() {
  return useMutation({
    mutationFn: ({ desk, email }: { desk?: string; email: string }) =>
      request<{ expiresInSeconds: number }>("/desk/codes", { method: "POST", body: { email, ...(desk ? { desk } : {}) } }),
  });
}

export function useEnterWithCode() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ desk, email, code }: { desk?: string; email: string; code: string }) =>
      request<{ principal: unknown }>("/desk/sessions", { method: "POST", body: { email, code, ...(desk ? { desk } : {}) } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: meQueryKey }),
  });
}

/** Walks into a desk whose door is open: a name and an address, no code. */
export function useEnterOpenDoor() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ desk, email, name }: { desk: string; email: string; name: string }) =>
      request<{ principal: unknown }>("/desk/sessions", { method: "POST", body: { email, name, desk } }),
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
