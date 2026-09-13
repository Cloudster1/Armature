import { createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { Page, PageHeader } from "@/components/ui";
import { FieldList } from "@/features/fields/FieldList";

/** The fields every project of the organization records. */
export const orgFieldsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/fields",
  component: OrgFieldsPage,
});

function OrgFieldsPage() {
  const { data: access } = useAccess();
  return (
    <Page width="content">
      <PageHeader title="Fields" meta="What every project records about an issue beyond the standard fields. A project's own field is promoted here from its Fields page." />
      <FieldList canConfigure={Boolean(access?.canAdministerOrg)} />
    </Page>
  );
}
