import { Link, Outlet, createRoute, useLocation, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useMe } from "@/api/auth";
import { Page, PageHeader, Tabs } from "@/components/ui";
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

export const workflowsIndexRoute = createRoute({
  getParentRoute: () => workflowsRoute,
  path: "/",
  validateSearch: (search: Record<string, unknown>) => ({
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
  component: SchemesTab,
});

type TabId = "workflows" | "statuses" | "schemes";

const tabs: Array<{ value: TabId; label: string; to: string }> = [
  { value: "workflows", label: "Workflows", to: "/settings/workflows" },
  { value: "statuses", label: "Statuses", to: "/settings/workflows/statuses" },
  { value: "schemes", label: "Schemes", to: "/settings/workflows/schemes" },
];

function useCanConfigure(): boolean {
  const { data: me } = useMe();
  const role = me?.principal?.role;
  return role === "owner" || role === "admin";
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
  const { workflow: focused } = workflowsIndexRoute.useSearch();
  return <WorkflowCards focused={focused} canConfigure={useCanConfigure()} />;
}

function StatusesTab() {
  return <StatusCards canConfigure={useCanConfigure()} />;
}

function SchemesTab() {
  return <SchemeCards canConfigure={useCanConfigure()} />;
}
