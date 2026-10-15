import { useCallback } from "react";
import { useAccess } from "@/api/access";
import { Link, Outlet, createRoute, useLocation, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { Page, PageHeader, Tabs, isPageOffset } from "@/components/ui";
import { SchemeCards } from "@/features/workflows/library/SchemeCards";
import { StatusCards } from "@/features/workflows/library/StatusCards";
import { WorkflowCards } from "@/features/workflows/library/WorkflowCards";

/**
 * The organization's library: its workflows, the statuses they are built
 * from, and the schemes that hand them to issue types, as three tabs under
 * one head. The workflows come first because they are what the page is for.
 */
export const workflowsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/workflows",
  component: WorkflowLibrary,
});

/** What a library tab is searched for and which page it is on, in the address so a link reopens it. */
export interface LibrarySearch {
  q?: string;
  offset?: number;
}

function librarySearch(search: Record<string, unknown>): LibrarySearch {
  return {
    // A search of digits alone comes back from the address as a number.
    ...(typeof search.q === "number" ? { q: String(search.q) } : typeof search.q === "string" && search.q !== "" ? { q: search.q } : {}),
    ...(isPageOffset(search.offset) ? { offset: search.offset } : {}),
  };
}

/** A link to one workflow names it by id to mark its card and searches for its name to find it. */
export const workflowsIndexRoute = createRoute({
  getParentRoute: () => workflowsRoute,
  path: "/",
  validateSearch: (search: Record<string, unknown>): LibrarySearch & { workflow?: string } => ({
    ...librarySearch(search),
    workflow: typeof search.workflow === "string" ? search.workflow : undefined,
  }),
  component: WorkflowsTab,
});

export const workflowStatusesRoute = createRoute({
  getParentRoute: () => workflowsRoute,
  path: "/statuses",
  component: StatusesTab,
});

export const workflowSchemesRoute = createRoute({
  getParentRoute: () => workflowsRoute,
  path: "/schemes",
  validateSearch: librarySearch,
  component: SchemesTab,
});

type TabId = "workflows" | "statuses" | "schemes";

const tabs: Array<{ value: TabId; label: string; to: string }> = [
  { value: "workflows", label: "Workflows", to: "/settings/workflows" },
  { value: "statuses", label: "Statuses", to: "/settings/workflows/statuses" },
  { value: "schemes", label: "Schemes", to: "/settings/workflows/schemes" },
];

// What the Access page granted decides, not the seat somebody holds in the
// organization: a member made a global administrator configures workflows.
function useCanConfigure(): boolean {
  const { data: access } = useAccess();
  return access?.canAdministerOrg ?? false;
}

function WorkflowLibrary() {
  const pathname = useLocation({ select: (location) => location.pathname });
  const navigate = useNavigate();
  const current: TabId = pathname.endsWith("/statuses") ? "statuses" : pathname.endsWith("/schemes") ? "schemes" : "workflows";
  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Workflows"
        tabs={<Tabs label="Workflows" value={current} onChange={(next) => navigate({ to: tabs.find((t) => t.value === next)!.to as never })} tabs={tabs.map((t) => ({ value: t.value, label: t.label, attrs: { "data-workflow-tab": t.label } }))} />}
      />
      <Outlet />
    </Page>
  );
}

function WorkflowsTab() {
  const { workflow: focused, q = "", offset = 0 } = workflowsIndexRoute.useSearch();
  const navigate = useNavigate();
  // A new search or page leaves the marked card behind; the address keeps the rest.
  const show = useCallback(
    (next: LibrarySearch, replace = false) =>
      navigate({ to: "/settings/workflows", search: { q: next.q || undefined, offset: next.offset || undefined, workflow: replace ? focused : undefined }, replace }),
    [navigate, focused],
  );
  return <WorkflowCards focused={focused} q={q} offset={offset} onShow={show} canConfigure={useCanConfigure()} />;
}

function StatusesTab() {
  return <StatusCards canConfigure={useCanConfigure()} />;
}

function SchemesTab() {
  const { q = "", offset = 0 } = workflowSchemesRoute.useSearch();
  const navigate = useNavigate();
  const show = useCallback(
    (next: LibrarySearch, replace = false) =>
      navigate({ to: "/settings/workflows/schemes", search: { q: next.q || undefined, offset: next.offset || undefined }, replace }),
    [navigate],
  );
  return <SchemeCards q={q} offset={offset} onShow={show} canConfigure={useCanConfigure()} />;
}
