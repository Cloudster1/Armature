import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { meQueryKey } from "@/api/auth";

/**
 * A wrapper whose session is a reader with this language and zone, so a
 * component formats as it would for them without the network or a mock.
 */
export function asReader(locale: string, timezone = "UTC") {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  client.setQueryData(meQueryKey, {
    principal: { user: { id: "reader", email: "reader@example.com", name: "Reader", timezone, locale, isActive: true } },
    organizations: [],
  });
  return function Reader({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}
