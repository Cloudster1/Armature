import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { request } from "./client";
import { META_STALE_MS } from "@/config";

export interface Place {
  id: string;
  title: string;
  sentence: string;
  path: string;
}

export interface AssistantAnswer {
  text: string;
  to?: string;
  query?: string;
}

/** Whether this deployment has a model behind Ask; the palette offers it only then. */
export function useAssistantStatus() {
  return useQuery({
    queryKey: ["assistant"],
    queryFn: () => request<{ configured: boolean }>("/assistant"),
    staleTime: META_STALE_MS,
  });
}

/** A change the model asked for. It happens when the reader confirms it, not before. */
export interface AssistantProposal {
  tool: string;
  says: string;
  arguments?: Record<string, unknown>;
}

export function useAsk() {
  return useMutation({
    mutationFn: (input: { question: string; projectKey?: string; places: Place[] }) =>
      request<{ answer: AssistantAnswer; proposals?: AssistantProposal[] }>("/assistant/ask", { method: "POST", body: input }),
  });
}

/** Makes a proposed change, through the same door any client would use. */
export function useConfirmProposal() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (p: AssistantProposal) =>
      request<{ result?: { isError?: boolean } }>("/mcp", {
        method: "POST",
        body: { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: p.tool, arguments: p.arguments ?? {} } },
      }),
    onSuccess: () => client.invalidateQueries(),
  });
}
