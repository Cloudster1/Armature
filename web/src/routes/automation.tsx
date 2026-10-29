import { Link, createRoute } from "@tanstack/react-router";
import { projectSettingsRoute } from "./project-settings";
import { adminRoute } from "./admin";
import { canAdminister, useAccess } from "@/api/access";
import { useMe } from "@/api/auth";
import { Page, PageHeader } from "@/components/ui";
import { RulesPanel } from "@/features/automation/RulesPanel";
import { t } from "@/i18n";

/** A project's rules: what it does by itself. */
export const automationRoute = createRoute({
  getParentRoute: () => projectSettingsRoute,
  path: "/automation",
  component: ProjectAutomationPage,
});

/** The organization's rules, which watch every project. */
export const orgAutomationRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/automation",
  component: OrgAutomationPage,
});

function ProjectAutomationPage() {
  const { projectKey } = automationRoute.useParams();
  const { data: access } = useAccess();
  return (
    <>
      <p className="mb-4 text-sm text-ink-muted">{t.projectSetup.automation.projectMeta}</p>
      <RulesPanel projectKey={projectKey} canEdit={Boolean(canAdminister(access, projectKey))} />
    </>
  );
}

function OrgAutomationPage() {
  const { data } = useMe();
  const { data: access } = useAccess();
  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/admin" className="hover:text-ink">
            {t.shell.admin.crumb}
          </Link>
        }
        title={t.projectSetup.automation.title}
        meta={data?.principal?.installation?.name ? t.projectSetup.automation.orgMeta(data.principal.installation.name) : undefined}
      />
      <RulesPanel canEdit={Boolean(access?.canAdministerOrg)} />
    </Page>
  );
}
