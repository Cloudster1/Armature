import { createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { EmptyState, Page, PageHeader } from "@/components/ui";
import { OrgArrangement } from "@/features/arrange/pages";
import { t } from "@/i18n";

/** The arrangement every project of the organization follows until it disagrees. */
export const orgIssueArrangementRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/issue-view",
  component: OrgIssueArrangementPage,
});

function OrgIssueArrangementPage() {
  const { data: access } = useAccess();
  const canConfigure = Boolean(access?.canAdministerOrg);
  return (
    <Page width="content">
      <PageHeader title={t.looks.arrange.title} meta={t.looks.arrange.meta} />
      {canConfigure ? (
        <OrgArrangement canConfigure={canConfigure} />
      ) : (
        <EmptyState title={t.looks.arrange.onlyAdmins} description={t.looks.arrange.onlyAdminsDetail} />
      )}
    </Page>
  );
}
