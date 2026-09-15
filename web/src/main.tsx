import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";

import { buildRouter } from "./routes";
import { applyCustomTheme, applyTheme, readCachedTheme, readTheme } from "./lib/theme";
import "./styles/index.css";

// Apply the stored theme before the first paint so there is no flash of the
// wrong palette. A custom theme this browser saw last is applied the same
// way, and replaced once the server has said which one is current.
applyTheme(readTheme());
const cached = readCachedTheme();
if (cached) applyCustomTheme(cached.css);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A failed request is usually a real answer (401, 404, validation), not
      // a blip worth retrying three times while the user waits.
      retry: false,
      refetchOnWindowFocus: false,
      staleTime: 10_000,
    },
  },
});

const router = buildRouter(queryClient);

const container = document.getElementById("root");
if (!container) throw new Error("root element is missing from index.html");

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
