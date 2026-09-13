import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";

/**
 * A repository connected to a project. What flows in from it is linked to
 * issues by key; with a token, the tracker can reach back to it.
 */
export type HostKind = "github" | "gitlab" | "gitea";

/** The host's name as people write it. */
export function hostLabel(host: HostKind | string): string {
  return { github: "GitHub", gitlab: "GitLab", gitea: "Gitea" }[host] ?? host;
}

export interface Repository {
  id: string;
  projectId: string;
  projectKey: string;
  host: HostKind;
  /** "owner/name" on the host. */
  name: string;
  url: string;
  apiBaseUrl: string;
  defaultBranch: string;
  /** Whether the tracker can write to the host. The token itself never comes back. */
  hasToken: boolean;
  transitionOnMerge?: string;
  /** Present only on the response that made or rotated it. */
  webhookSecret?: string;
  commitCount: number;
  openPullRequestCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface Commit {
  id: string;
  repository: string;
  sha: string;
  message: string;
  authorName?: string;
  url?: string;
  branch?: string;
  committedAt: string;
}

/**
 * A branch made for, or named after, an issue. It is followed from then on:
 * pushes move its head and count, and merging the pull request from it marks
 * it merged.
 */
export interface Branch {
  id: string;
  repository: string;
  name: string;
  url?: string;
  headSha?: string;
  commitCount: number;
  pushedAt?: string;
  mergedAt?: string;
  createdAt: string;
}

export type CIStatus = "pending" | "success" | "failure" | "cancelled";

export interface CIRun {
  id: string;
  repository: string;
  externalId: string;
  name: string;
  status: CIStatus;
  url?: string;
  sha: string;
  branch?: string;
  startedAt: string;
  finishedAt?: string;
}

export interface PullRequest {
  id: string;
  repository: string;
  number: number;
  title: string;
  url?: string;
  state: "open" | "merged" | "closed";
  sourceBranch?: string;
  targetBranch?: string;
  authorName?: string;
  headSha?: string;
  openedAt: string;
  mergedAt?: string;
  /** The newest run against the pull request's head. */
  ci?: CIRun;
}

/** Everything the repositories know about one issue. */
export interface Development {
  /** The name a branch made for the issue would get. */
  branchName: string;
  branches: Branch[];
  pullRequests: PullRequest[];
  commits: Commit[];
  runs: CIRun[];
}

export const gitQueryKey = ["git"] as const;

export function useRepositories(projectKey: string) {
  return useQuery({
    queryKey: [...gitQueryKey, "repositories", projectKey],
    queryFn: () => request<{ repositories: Repository[] }>(`/projects/${projectKey}/repositories`),
    enabled: Boolean(projectKey),
  });
}

export function useDevelopment(issueKey: string) {
  return useQuery({
    queryKey: [...gitQueryKey, "development", issueKey],
    queryFn: () => request<Development>(`/issues/${issueKey}/development`),
    enabled: Boolean(issueKey),
  });
}

function useGitMutation<TArgs, TResult>(run: (args: TArgs) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: gitQueryKey }),
  });
}

export interface ConnectInput {
  host: HostKind;
  name: string;
  url?: string;
  apiBaseUrl?: string;
  defaultBranch?: string;
  accessToken?: string;
  transitionOnMerge?: string;
}

/** The response that carries the one-time secret and the address to give the host. */
export interface Connected {
  repository: Repository;
  webhookUrl: string;
}

export function useConnectRepository(projectKey: string) {
  return useGitMutation((input: ConnectInput) =>
    request<Connected>(`/projects/${projectKey}/repositories`, { method: "POST", body: input }),
  );
}

export function useUpdateRepository() {
  return useGitMutation(({ id, ...input }: Partial<ConnectInput> & { id: string }) =>
    request<{ repository: Repository }>(`/repositories/${id}`, { method: "PATCH", body: input }),
  );
}

export function useRotateSecret() {
  return useGitMutation((id: string) =>
    request<Connected>(`/repositories/${id}/rotate-secret`, { method: "POST" }),
  );
}

export function useDisconnectRepository() {
  return useGitMutation((id: string) => request<void>(`/repositories/${id}`, { method: "DELETE" }));
}

export interface CreateBranchInput {
  repositoryId: string;
  /** Empty takes the name suggested for the issue. */
  name?: string;
  /** Empty starts from the repository's default branch. */
  from?: string;
}

export function useCreateBranch(issueKey: string) {
  return useGitMutation((input: CreateBranchInput) =>
    request<{ branch: Branch }>(`/issues/${issueKey}/branches`, { method: "POST", body: input }),
  );
}

/** What to type to start working on a branch that now exists on the host. */
export function checkoutCommand(branch: string): string {
  return `git fetch origin && git switch ${branch}`;
}

/** The host's own word for a pull request. */
export function pullWord(host: HostKind | string): string {
  return host === "gitlab" ? "merge request" : "pull request";
}
