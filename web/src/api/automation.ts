import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";

export interface RuleOption {
  name: string;
  label: string;
  kind: "text" | "roles";
  required: boolean;
  choices: string[];
}

/** What the editor offers for a trigger, a condition or an action. */
export interface RuleDescriptor {
  kind: "trigger" | "condition" | "action";
  type: string;
  label: string;
  description: string;
  options: RuleOption[];
  needsIssue?: boolean;
}

export interface Schedule {
  unit: "hours" | "daily" | "weekly";
  every?: number;
  at?: string;
  weekday?: number;
}

export interface Trigger {
  kind: string;
  field?: string;
  query?: string;
  schedule?: Schedule;
  token?: string;
}

export interface Condition {
  kind: string;
  query?: string;
  field?: string;
  value?: string;
  roles?: string[];
}

export interface Action {
  kind: string;
  field?: string;
  value?: string;
  text?: string;
  subject?: string;
  to?: string;
  endpointId?: string;
}

export interface Rule {
  id: string;
  projectId?: string;
  projectKey?: string;
  name: string;
  enabled: boolean;
  trigger: Trigger;
  conditions: Condition[];
  actions: Action[];
  allowOwnEvents: boolean;
  hourlyCap: number;
  createdAt: string;
  updatedAt: string;
  lastRunAt?: string;
}

export interface RuleInput {
  name: string;
  enabled?: boolean;
  trigger: Trigger;
  conditions: Condition[];
  actions: Action[];
  allowOwnEvents: boolean;
  hourlyCap?: number;
}

export interface RuleRun {
  id: string;
  ruleId: string;
  eventId: string;
  issueKey?: string;
  startedAt: string;
  finishedAt?: string;
  outcome: "running" | "done" | "skipped" | "failed" | "capped";
  reason?: string;
  actions: Array<{ kind: string; note: string; ok: boolean }>;
}

export function useAutomationCatalog() {
  return useQuery({
    queryKey: ["automation", "catalog"],
    queryFn: () => request<{ catalog: RuleDescriptor[]; topics: string[] }>("/automation/catalog"),
    staleTime: META_STALE_MS,
  });
}

/** A project's rules, or with no key the organization's own. */
export function useRules(projectKey?: string) {
  return useQuery({
    queryKey: ["automation", "rules", projectKey ?? ""],
    queryFn: () => request<{ rules: Rule[] }>(projectKey ? `/projects/${projectKey}/automation/rules` : "/automation/rules"),
  });
}

export function useRuleRuns(ruleId: string | null) {
  return useQuery({
    queryKey: ["automation", "runs", ruleId],
    queryFn: () => request<{ runs: RuleRun[] }>(`/automation/rules/${ruleId}/runs`),
    enabled: ruleId !== null,
  });
}

function useRuleMutation<TInput, TOut>(fn: (input: TInput) => Promise<TOut>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: fn, onSuccess: () => queryClient.invalidateQueries({ queryKey: ["automation"] }) });
}

export function useCreateRule(projectKey?: string) {
  return useRuleMutation((input: RuleInput) =>
    request<{ rule: Rule }>(projectKey ? `/projects/${projectKey}/automation/rules` : "/automation/rules", { method: "POST", body: input }),
  );
}

export function useUpdateRule() {
  return useRuleMutation((input: { id: string } & RuleInput) => {
    const { id, ...body } = input;
    return request<{ rule: Rule }>(`/automation/rules/${id}`, { method: "PATCH", body });
  });
}

export function useDeleteRule() {
  return useRuleMutation((id: string) => request<void>(`/automation/rules/${id}`, { method: "DELETE" }));
}

export function useRunRule() {
  return useRuleMutation((input: { id: string; issueKey?: string }) =>
    request<{ run: RuleRun }>(`/automation/rules/${input.id}/run`, { method: "POST", body: { issueKey: input.issueKey } }),
  );
}

// Webhooks: where events go out.

export interface Webhook {
  id: string;
  name: string;
  url: string;
  topics: string[];
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
  /** Only on the answer that made or rotated it. */
  secret?: string;
}

export interface WebhookInput {
  name: string;
  url: string;
  topics: string[];
  enabled?: boolean;
}

export interface Delivery {
  id: string;
  endpointId: string;
  eventId: string;
  topic: string;
  attempt: number;
  status?: number;
  error?: string;
  nextAttemptAt?: string;
  deliveredAt?: string;
  createdAt: string;
}

export function useWebhooks() {
  return useQuery({ queryKey: ["webhooks"], queryFn: () => request<{ webhooks: Webhook[] }>("/webhooks") });
}

export function useDeliveries(endpointId: string | null) {
  return useQuery({
    queryKey: ["webhooks", "deliveries", endpointId],
    queryFn: () => request<{ deliveries: Delivery[] }>(`/webhooks/${endpointId}/deliveries`),
    enabled: endpointId !== null,
  });
}

function useWebhookMutation<TInput, TOut>(fn: (input: TInput) => Promise<TOut>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: fn, onSuccess: () => queryClient.invalidateQueries({ queryKey: ["webhooks"] }) });
}

export function useCreateWebhook() {
  return useWebhookMutation((input: WebhookInput) => request<{ webhook: Webhook }>("/webhooks", { method: "POST", body: input }));
}

export function useUpdateWebhook() {
  return useWebhookMutation((input: { id: string } & WebhookInput) => {
    const { id, ...body } = input;
    return request<{ webhook: Webhook }>(`/webhooks/${id}`, { method: "PATCH", body });
  });
}

export function useDeleteWebhook() {
  return useWebhookMutation((id: string) => request<void>(`/webhooks/${id}`, { method: "DELETE" }));
}

export function useRotateWebhookSecret() {
  return useWebhookMutation((id: string) => request<{ webhook: Webhook }>(`/webhooks/${id}/rotate-secret`, { method: "POST" }));
}

export function useTestWebhook() {
  return useWebhookMutation((id: string) => request<{ delivery: Delivery }>(`/webhooks/${id}/test`, { method: "POST" }));
}

export function useRedeliver() {
  return useWebhookMutation((input: { endpointId: string; deliveryId: string }) =>
    request<{ delivery: Delivery }>(`/webhooks/${input.endpointId}/deliveries/${input.deliveryId}/redeliver`, { method: "POST" }),
  );
}
