import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { request } from "./client";
import { meQueryKey } from "./auth";
import type { Project } from "./projects";

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

/** Fills the installation with the demo, whose projects, labels and workflow every list may show. */
export function useAddDemoProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => request<{ project: Project }>("/installation/demo-project", { method: "POST" }),
    onSuccess: () => queryClient.invalidateQueries(),
  });
}
