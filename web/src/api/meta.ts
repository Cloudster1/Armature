import { useQuery } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";

export interface Build {
  version: string;
  commit?: string;
  builtAt?: string;
}

/** The build of the API that is answering, for the foot of the screen. */
export function useBuild() {
  return useQuery({
    queryKey: ["build"],
    queryFn: () => request<{ build: Build }>("/build"),
    staleTime: META_STALE_MS,
  });
}
