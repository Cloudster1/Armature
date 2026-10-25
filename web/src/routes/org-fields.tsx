import { createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { Page, PageHeader } from "@/components/ui";
import { FieldList } from "@/features/fields/FieldList";
import { t } from "@/i18n";

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
      <PageHeader title={t.looks.fields.title} meta={t.looks.fields.everyoneMeta} />
      <FieldList canConfigure={Boolean(access?.canAdministerOrg)} />
    </Page>
  );
}
