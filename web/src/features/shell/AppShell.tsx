import { Link, Navigate, Outlet, useLocation, useNavigate, type ErrorComponentProps } from "@tanstack/react-router";
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { useMe } from "@/api/auth";
import { Button, EmptyState, IconButton, PageHeader, Skeleton, Spotlight, ToastProvider, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { MutationErrorToasts } from "@/features/shell/MutationErrorToasts";
import { Sidebar, SidebarFoot, useCurrentProjectKey } from "@/features/shell/Sidebar";
import { NotificationsBell } from "@/features/shell/nav";
import { PageCrumbsContext, ShellHeaderContext } from "@/components/ui";
import { PlaceCrumbs } from "@/features/shell/Crumbs";
import { SkipLink } from "@/features/shell/SkipLink";
import { DRAWER_ID, MAIN_ID, rememberOutsideSettings, useLastProject, useNarrow } from "@/features/shell/state";
import { inSettingsArea } from "@/features/shell/places";
import { CommandPalette, usePaletteShortcut, type PaletteMode, type SpotlightRequest } from "@/features/palette/CommandPalette";
import { CreateIssueDialog } from "@/features/issues/CreateIssueDialog";
import { IssueDrawerProvider } from "@/features/issues/IssueDrawer";
import { IssuePanel } from "@/features/issues/IssuePanel";
import { EnrolmentRequired } from "@/features/auth/SecondFactor";
import { ThemeLoader } from "@/features/themes/ThemeLoader";
import { BackdropEffect } from "@/features/themes/BackdropEffect";
import { t } from "@/i18n";

/** Set inside the signed-in shell, so a page drawn in it does not draw the shell again. */
const InShell = createContext(false);

// A page that throws loses only itself: the shell stays, and the reader can go
// somewhere else or try again.
export function RouteError({ error, reset }: ErrorComponentProps) {
  return (
    <div data-route-error>
      <PageHeader title={t.shell.page.notShown} />
      <EmptyState
        icon={<Icon.Warning />}
        title={t.shell.page.notShownTitle}
        description={error instanceof Error && error.message ? error.message : t.shell.page.notShownFallback}
        action={
          <Button variant="secondary" onClick={reset}>
            {t.shell.page.tryAgain}
          </Button>
        }
      />
    </div>
  );
}

/** A path that leads nowhere, drawn as a page: its head, what happened and the way home. */
export function NotFound() {
  const navigate = useNavigate();
  return (
    <div data-not-found>
      <PageHeader title={t.doors.notFound.title} />
      <EmptyState
        icon={<Icon.Warning />}
        title={t.doors.notFound.emptyTitle}
        description={t.doors.notFound.body}
        action={
          <Button variant="secondary" onClick={() => navigate({ to: "/" })} data-action="home">
            {t.doors.notFound.home}
          </Button>
        }
      />
    </div>
  );
}

/**
 * The router's answer to a path that leads nowhere. Somebody signed in gets it
 * inside the shell, as Stator and Exciter do, so the sidebar is still there to
 * go somewhere else; anybody else gets it on its own.
 */
export function ShellNotFound() {
  const inShell = useContext(InShell);
  const { data, isPending } = useMe();
  if (inShell) return <NotFound />;
  if (data?.principal) {
    return (
      <AppShell>
        <NotFound />
      </AppShell>
    );
  }
  if (isPending) return <Skeleton lines={4} className="p-8" />;
  return (
    <div className="flex h-full items-center justify-center p-8 text-center" data-not-found>
      <div>
        <p className="text-lg font-semibold text-ink">{t.doors.notFound.title}</p>
        <p className="mt-1 text-sm text-ink-muted">{t.doors.notFound.body}</p>
      </div>
    </div>
  );
}

/** The signed-in layout around a page: the agents' shell, or the portal's for a customer. */
export function AppShell({ children }: { children?: ReactNode }) {
  const { data } = useMe();
  if (data?.principal?.role === "customer") return <PortalShell>{children}</PortalShell>;
  // Held until two-step sign-in is set up: nothing else would load.
  if (data?.principal?.mustEnrol) return <EnrolmentRequired />;
  return (
    <ToastProvider>
      <MutationErrorToasts />
      <ConfirmProvider>
        <IssueDrawerProvider>
          <InShell.Provider value>
            <AgentShell>{children}</AgentShell>
          </InShell.Provider>
        </IssueDrawerProvider>
      </ConfirmProvider>
    </ToastProvider>
  );
}

function AgentShell({ children }: { children?: ReactNode }) {
  const [palette, setPalette] = useState<PaletteMode | null>(null);
  const [creating, setCreating] = useState(false);
  const [spotlight, setSpotlight] = useState<SpotlightRequest | null>(null);
  const toast = useToast();
  const projectKey = useLastProject(useCurrentProjectKey());
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
  const narrow = useNarrow();
  const [drawer, setDrawer] = useState(false);
  const closeDrawer = useCallback(() => setDrawer(false), []);
  const location = useLocation();
  // Following a link closes the drawer; adjusted during render, so the drawer
  // is never drawn over the new page.
  const [drawerHref, setDrawerHref] = useState(location.href);
  if (drawerHref !== location.href) {
    setDrawerHref(location.href);
    setDrawer(false);
  }
  useEffect(() => {
    if (!inSettingsArea(location.pathname)) rememberOutsideSettings(location.href);
  }, [location.pathname, location.href]);
  return (
    <div className="flex h-full" data-shell={narrow ? "narrow" : "wide"}>
      <SkipLink />
      <ThemeLoader />
      <Sidebar onNewIssue={() => setCreating(true)} onAsk={() => setPalette("ask")} drawerOpen={narrow && drawer} onCloseDrawer={closeDrawer} />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {narrow && <NarrowStrip open={drawer} onOpen={() => setDrawer(true)} />}
        {/* Focusable only from the skip link, and not ringed then: it is where reading starts, not a control. */}
        <main id={MAIN_ID} tabIndex={-1} className="relative isolate min-h-0 min-w-0 flex-1 overflow-auto bg-backdrop focus:outline-none" data-backdrop-host>
          <BackdropEffect />
          <div
            ref={setStrip}
            className="sticky top-0 z-20 border-b border-border/60 bg-surface-glass px-4 pt-4 backdrop-blur-md empty:hidden shell:px-8 shell:pt-5"
            data-shell-header
          />
          <ShellHeaderContext.Provider value={strip}>
            <PageCrumbsContext.Provider value={<PlaceCrumbs />}>
              <div className="px-4 py-4 shell:px-8 shell:py-6" data-shell-content>
                {children ?? <Outlet />}
              </div>
            </PageCrumbsContext.Provider>
          </ShellHeaderContext.Provider>
        </main>
      </div>
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

/** What is left of the chrome on a phone: the button that opens the drawer, the installation and the bell. */
function NarrowStrip({ open, onOpen }: { open: boolean; onOpen: () => void }) {
  const { data } = useMe();
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-surface px-2" data-narrow-strip data-print-hide>
      <IconButton
        icon={<Icon.Menu />}
        label={t.shell.nav.openNavigation}
        size="sm"
        onClick={onOpen}
        aria-controls={DRAWER_ID}
        aria-expanded={open}
        data-action="open-drawer"
      />
      <span className="min-w-0 flex-1 truncate text-sm font-semibold text-ink">{data?.principal?.installation?.name}</span>
      <NotificationsBell />
    </div>
  );
}

/**
 * The portal's chrome: the installation's name, the two things a customer
 * does, and a way out. Nothing about projects, boards or settings, which are not theirs.
 */
function PortalShell({ children }: { children?: ReactNode }) {
  const { data } = useMe();
  const { pathname } = useLocation();
  const principal = data?.principal;
  // The agents' pages send a customer to the portal. Anything else, such as
  // the sign-in page a logout is heading for, is left alone.
  const agentPage = pathname === "/" || ["/projects", "/issues", "/settings", "/admin"].some((p) => pathname.startsWith(p));
  if (agentPage) return <Navigate to="/portal" />;
  // Taking a file back asks first, the same dialog the agents get.
  return (
    <ToastProvider>
      <MutationErrorToasts />
      <ConfirmProvider>
        <div className="flex h-full flex-col">
          <SkipLink />
          <header className="flex h-12 shrink-0 items-center justify-between border-b border-border bg-surface px-4 shell:px-6" data-print-hide>
            <div className="flex min-w-0 items-center gap-2 shell:gap-5">
              <span className="min-w-0 truncate text-sm font-semibold text-ink">{principal?.installation?.name ?? t.desk.portal.support}</span>
              <PortalLink to="/portal" exact>
                {t.desk.portal.yourRequests}
              </PortalLink>
              <PortalLink to="/portal/new">{t.desk.portal.raise}</PortalLink>
            </div>
            <div className="flex shrink-0 items-center gap-3">
              {/* On a phone the address gives way to the two links; the avatar still says who is in. */}
              <span className="hidden text-xs text-ink-subtle shell:inline" data-portal-address>
                {principal?.user.email}
              </span>
              <SidebarFoot userName={principal?.user.name ?? ""} avatar={principal?.user.avatarUrl} />
            </div>
          </header>
          <main id={MAIN_ID} tabIndex={-1} className="min-h-0 flex-1 overflow-auto focus:outline-none">
            <div className="px-4 py-4 shell:px-8 shell:py-6" data-shell-content>
              <PageCrumbsContext.Provider value={<PlaceCrumbs />}>
                <InShell.Provider value>{children ?? <Outlet />}</InShell.Provider>
              </PageCrumbsContext.Provider>
            </div>
          </main>
        </div>
      </ConfirmProvider>
    </ToastProvider>
  );
}

function PortalLink({ to, exact = false, children }: { to: string; exact?: boolean; children: ReactNode }) {
  return (
    <Link
      to={to}
      activeOptions={{ exact }}
      className="shrink-0 rounded-control px-2 py-1 text-sm whitespace-nowrap text-ink-muted hover:bg-surface-raised hover:text-ink"
      activeProps={{ className: "bg-accent-subtle text-accent font-medium hover:bg-accent-subtle hover:text-accent" }}
    >
      {children}
    </Link>
  );
}
