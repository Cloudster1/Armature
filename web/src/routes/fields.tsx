import { createRoute } from "@tanstack/react-router";
import { projectSettingsRoute } from "./project-settings";
import { canAdminister, useAccess } from "@/api/access";
import { FieldList } from "@/features/fields/FieldList";
import { t } from "@/i18n";

export const fieldsRoute = createRoute({
  getParentRoute: () => projectSettingsRoute,
  path: "/fields",
  component: FieldsPage,
});

function FieldsPage() {
  const { projectKey } = fieldsRoute.useParams();
  const { data: access } = useAccess();

  return (
    <>
      <p className="mb-4 text-sm text-ink-muted">{t.looks.fields.projectMeta}</p>
      <FieldList projectKey={projectKey} canConfigure={canAdminister(access, projectKey)} canPromote={Boolean(access?.canAdministerOrg)} />
    </>
  );
}
