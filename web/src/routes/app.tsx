import { Link, Navigate, Outlet, createRoute, redirect, useLocation } from "@tanstack/react-router";
import { useCallback, useState } from "react";
import { rootRoute } from "./root";
import { meQueryKey, useMe } from "@/api/auth";
import { request, ApiError } from "@/api/client";
import { Button, EmptyState, Spotlight, ToastProvider, useToast } from "@/components/ui";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { Sidebar, SidebarFoot, currentProjectKey } from "@/features/shell/Sidebar";
import { ShellHeaderContext } from "@/components/ui";
import { useLastProject } from "@/features/shell/state";
import { CommandPalette, usePaletteShortcut, type PaletteMode, type SpotlightRequest } from "@/features/palette/CommandPalette";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { IssueDrawerProvider } from "@/features/issues/IssueDrawer";
import { IssuePanel } from "@/features/issues/IssuePanel";

/**
 * The authenticated shell. Everything under it can assume a signed-in user in
 * an organization, because this guard has already run.
 */
export const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData({
        queryKey: meQueryKey,
        queryFn: () => request("/auth/me"),
        retry: false,
      });
    } catch (error) {
      if (error instanceof ApiError && error.isUnauthenticated) {
        // Remember where they were headed so the sign-in can return them there.
        throw redirect({ to: "/login", search: { next: location.pathname } });
      }
      throw error;
    }
  },
  component: AppShell,
  errorComponent: RouteError,
});

// A page that throws loses only itself: the shell stays, and the reader can go
// somewhere else or try again.
function RouteError({ error, reset }: { error: Error; reset: () => void }) {
  return (
    <div className="p-8">
      <EmptyState
        title="This page could not be shown"
        description={error.message}
        action={
          <Button variant="secondary" onClick={reset}>
            Try again
          </Button>
        }
      />
    </div>
  );
}

function AppShell() {
  const { data } = useMe();
  if (data?.principal?.role === "customer") return <PortalShell />;
  return (
    <ToastProvider>
      <ConfirmProvider>
        <IssueDrawerProvider>
          <AgentShell />
        </IssueDrawerProvider>
      </ConfirmProvider>
    </ToastProvider>
  );
}

function AgentShell() {
  const [palette, setPalette] = useState<PaletteMode | null>(null);
  const [creating, setCreating] = useState(false);
  const [spotlight, setSpotlight] = useState<SpotlightRequest | null>(null);
  const toast = useToast();
  const projectKey = useLastProject(currentProjectKey());
  const openPalette = useCallback(() => setPalette("command"), []);
  usePaletteShortcut(openPalette);
  const clearSpotlight = useCallback(() => setSpotlight(null), []);
  // The page the answer named has no such element: the sentence still reaches the reader.
  const spotlightMissing = useCallback(() => {
    if (spotlight) toast.info(spotlight.text);
    setSpotlight(null);
  }, [spotlight, toast]);
  // The page's head is drawn into the strip over the content, which floats
  // as the content scrolls under it; the ref lands before first paint.
  const [strip, setStrip] = useState<HTMLElement | null>(null);
  return (
    <div className="flex h-full">
      <Sidebar onNewIssue={() => setCreating(true)} onAsk={() => setPalette("ask")} />
      <main className="min-h-0 min-w-0 flex-1 overflow-auto bg-backdrop">
        <div ref={setStrip} className="sticky top-0 z-20 border-b border-border/60 bg-surface-glass px-8 pt-5 backdrop-blur-md empty:hidden" data-shell-header />
        <ShellHeaderContext.Provider value={strip}>
          <div className="px-8 py-6">
            <Outlet />
          </div>
        </ShellHeaderContext.Provider>
      </main>
      <IssuePanel />
      <CommandPalette
        open={palette !== null}
        mode={palette ?? "command"}
        onClose={() => setPalette(null)}
        projectKey={projectKey}
        onNewIssue={() => setCreating(true)}
        onToggleSidebar={() => document.querySelector<HTMLButtonElement>('[data-action="sidebar"]')?.click()}
        onSpotlight={setSpotlight}
      />
      {spotlight && <Spotlight target={spotlight.target} text={spotlight.text} onDone={clearSpotlight} onMissing={spotlightMissing} />}
      <CreateIssueDialog open={creating} onClose={() => setCreating(false)} projectKey={projectKey} />
    </div>
  );
}

/**
 * The portal's chrome: the organization, the two things a customer does, and
 * a way out. Nothing about projects, boards or settings, which are not theirs.
 */
function PortalShell() {
  const { data } = useMe();
  const { pathname } = useLocation();
  const principal = data?.principal;
  // The agents' pages send a customer to the portal. Anything else, such as
  // the sign-in page a logout is heading for, is left alone.
  const agentPage = pathname === "/" || ["/projects", "/issues", "/settings"].some((p) => pathname.startsWith(p));
  if (agentPage) return <Navigate to="/portal" />;
  // Taking a file back asks first, the same dialog the agents get.
  return (
    <ToastProvider>
      <ConfirmProvider>
      <div className="flex h-full flex-col">
        <header className="flex h-12 shrink-0 items-center justify-between border-b border-border bg-surface px-6">
          <div className="flex items-center gap-5">
            <span className="text-sm font-semibold text-ink">{principal?.org?.name ?? "Support"}</span>
            <PortalLink to="/portal" exact>
              Your requests
            </PortalLink>
            <PortalLink to="/portal/new">Raise a request</PortalLink>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-xs text-ink-subtle" data-portal-address>
              {principal?.user.email}
            </span>
            <SidebarFoot userName={principal?.user.name ?? ""} role={principal?.role ?? ""} avatar={principal?.user.avatarUrl} compact />
          </div>
        </header>
        <main className="min-h-0 flex-1 overflow-auto">
          <div className="px-8 py-6">
            <Outlet />
          </div>
        </main>
      </div>
      </ConfirmProvider>
    </ToastProvider>
  );
}

function PortalLink({ to, exact = false, children }: { to: string; exact?: boolean; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      activeOptions={{ exact }}
      className="rounded-control px-2 py-1 text-sm text-ink-muted hover:bg-surface-raised hover:text-ink"
      activeProps={{ className: "bg-accent-subtle text-accent font-medium hover:bg-accent-subtle hover:text-accent" }}
    >
      {children}
    </Link>
  );
}
