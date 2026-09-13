import { createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { EmptyState, Page, PageHeader } from "@/components/ui";
import { OrgArrangement } from "@/features/arrange/pages";

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
      <PageHeader title="Issue view" meta="Which fields an issue shows, where, and in what order, for every project that has not arranged its own." />
      {canConfigure ? (
        <OrgArrangement canConfigure={canConfigure} />
      ) : (
        <EmptyState title="Only the organization's administrators arrange this" description="A project's own administrators arrange that project's issues." />
      )}
    </Page>
  );
}
