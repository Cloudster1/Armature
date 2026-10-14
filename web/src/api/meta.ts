import { useQuery } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";

export interface Build {
  version: string;
  /** Only for somebody signed in. */
  commit?: string;
  builtAt?: string;
}

export interface Meta {
  product: string;
  build: Build;
  apiVersion: string;
  capabilities: string[];
}

/** Which build of the API is answering and what it speaks, for the foot of the screen. */
export function useMeta() {
  return useQuery({
    queryKey: ["meta"],
    queryFn: () => request<Meta>("/meta"),
    staleTime: META_STALE_MS,
  });
}
