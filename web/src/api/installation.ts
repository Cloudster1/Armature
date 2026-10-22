import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import { meQueryKey } from "./auth";
import type { Project } from "./projects";
import type { components } from "./schema";
import { SUITE_DEMO_POLL_MS } from "@/config";

/** What everybody signed in may read about the installation. */
export interface Installation {
  name: string;
}

const installationQueryKey = ["installation"] as const;

export function useInstallation() {
  return useQuery({
    queryKey: installationQueryKey,
    queryFn: () => request<{ installation: Installation }>("/installation"),
  });
}

/** Renames the installation; the session reads the name too, for the sidebar's head. */
export function useRenameInstallation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => request<{ installation: Installation }>("/installation", { method: "PUT", body: { name } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: installationQueryKey });
      void queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

/** Another tool of the suite this installation is connected to, at its connected app's address. */
export type ConnectedTool = components["schemas"]["ConnectedTool"];

/** The tools the avatar menu offers to switch to; everyone signed in may read them. */
export function useConnectedTools() {
  return useQuery({
    queryKey: ["installation", "tools"] as const,
    queryFn: () => request<{ tools: ConnectedTool[] }>("/installation/tools"),
    select: (data) => data.tools,
  });
}

/** Fills the installation with the demo, whose projects, labels and workflow every list may show. */
export function useAddDemoProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<{ project: Project }>("/installation/demo-project", { method: "POST" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}

/** How far the suite demo got: the GitHub issues of Armature, Exciter and Stator, which the worker builds. */
export type SuiteDemo = components["schemas"]["SuiteDemo"];

const suiteDemoQueryKey = ["installation", "suite-demo"] as const;

/** The suite demo, null until somebody added it; asked again while the worker builds it. */
export function useSuiteDemo(enabled: boolean) {
  return useQuery({
    queryKey: suiteDemoQueryKey,
    queryFn: () => request<{ demo: SuiteDemo | null }>("/installation/suite-demo"),
    enabled,
    refetchInterval: (query) => {
      const state = query.state.data?.demo?.state;
      return state === "queued" || state === "running" ? SUITE_DEMO_POLL_MS : false;
    },
  });
}

/** Asks the worker for the suite demo; its projects appear as it builds them. */
export function useAddSuiteDemo() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<{ demo: SuiteDemo }>("/installation/suite-demo", { method: "POST" }),
    onSuccess: (data) => queryClient.setQueryData(suiteDemoQueryKey, data),
  });
}
