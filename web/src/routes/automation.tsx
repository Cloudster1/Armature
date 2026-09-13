import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { appRoute } from "./app";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { useMe } from "@/api/auth";
import { Page, PageHeader } from "@/components/ui";
import { RulesPanel } from "@/features/automation/RulesPanel";

/** A project's rules: what it does by itself. */
export const automationRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/automation",
  component: ProjectAutomationPage,
});

/** The organization's rules, which watch every project. */
export const orgAutomationRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/automation",
  component: OrgAutomationPage,
});

function ProjectAutomationPage() {
  const { projectKey } = automationRoute.useParams();
  const { data } = useProject(projectKey);
  const { data: access } = useAccess();
  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Automation"
        meta="Rules that run when something happens here"
      />
      <RulesPanel projectKey={projectKey} canEdit={Boolean(canAdminister(access, projectKey))} />
    </Page>
  );
}

function OrgAutomationPage() {
  const { data } = useMe();
  const { data: access } = useAccess();
  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Automation"
        meta={data?.principal?.org?.name ? `Rules across every project of ${data.principal.org.name}` : undefined}
      />
      <RulesPanel canEdit={Boolean(access?.canAdministerOrg)} />
    </Page>
  );
}
