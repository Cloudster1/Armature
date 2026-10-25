import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { appRoute } from "./app";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { useMe } from "@/api/auth";
import { Page, PageHeader } from "@/components/ui";
import { RulesPanel } from "@/features/automation/RulesPanel";
import { t } from "@/i18n";

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
        title={t.projectSetup.automation.title}
        meta={t.projectSetup.automation.projectMeta}
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
            {t.settings.crumb}
          </Link>
        }
        title={t.projectSetup.automation.title}
        meta={data?.principal?.installation?.name ? t.projectSetup.automation.orgMeta(data.principal.installation.name) : undefined}
      />
      <RulesPanel canEdit={Boolean(access?.canAdministerOrg)} />
    </Page>
  );
}
