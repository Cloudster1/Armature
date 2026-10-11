import { QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";
import { BASE } from "@/api/client";
import { ToastProvider } from "@/components/ui";
import { MutationErrorToasts } from "@/features/shell/MutationErrorToasts";
import { createQueryClient } from "@/lib/queryClient";

/** Renders under the same query client and toasts the app shell gives a page. */
export function renderInShell(ui: ReactElement) {
  const queryClient = createQueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <MutationErrorToasts />
        {ui}
      </ToastProvider>
    </QueryClientProvider>,
  );
}

/** What the server answers to "METHOD /path": a status and the JSON it sends. */
export type Routes = Record<string, [status: number, body?: unknown]>;

// The server stands in at fetch, so everything above it, the client's
// unwrapping included, runs as it does in the browser.
export function serve(routes: Routes) {
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${url.replace(BASE, "")}`;
    const route = routes[key];
    if (!route) throw new Error(`Nothing serves ${key} in this test.`);
    const [status, body] = route;
    return { status, ok: status >= 200 && status < 300, text: () => Promise.resolve(body === undefined ? "" : JSON.stringify(body)) };
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

/** The error envelope the API sends with a refusal. */
export function refusal(code: string, message: string) {
  return { error: { code, message } };
}
