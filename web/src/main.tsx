import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";

import { buildRouter } from "./routes";
import { LocalizedRouter } from "./features/shell/LocalizedRouter";
import { ToastShelf } from "./components/ui";
import { createQueryClient } from "./lib/queryClient";
import { trySilentSignIn } from "./lib/silentSignIn";
import { applyCustomTheme, applyTheme, readCachedTheme, readTheme } from "./lib/theme";
import "./styles/index.css";

// Apply the stored theme before the first paint so there is no flash of the
// wrong palette. A custom theme this browser saw last is applied the same
// way, and replaced once the server has said which one is current.
applyTheme(readTheme());
const cached = readCachedTheme();
if (cached) applyCustomTheme(cached.css);

// An ended session goes to sign in, which brings the person back to this page,
// quietly through the provider when it still knows them.
const queryClient = createQueryClient(() => {
  const next = router.state.location.href;
  void trySilentSignIn(next).then((leaving) => {
    if (!leaving) void router.navigate({ to: "/login", search: { next } });
  });
});

const router = buildRouter(queryClient);

const container = document.getElementById("root");
if (!container) throw new Error("root element is missing from index.html");

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      {/* Above the router, so a change of language keeps the toasts on screen. */}
      <ToastShelf>
        <LocalizedRouter router={router} />
      </ToastShelf>
    </QueryClientProvider>
  </StrictMode>,
);
