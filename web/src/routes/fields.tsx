import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { Page, PageHeader } from "@/components/ui";
import { FieldList } from "@/features/fields/FieldList";
import { t } from "@/i18n";

export const fieldsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/fields",
  component: FieldsPage,
});

function FieldsPage() {
  const { projectKey } = fieldsRoute.useParams();
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
        title={t.looks.fields.title}
        meta={t.looks.fields.projectMeta}
      />
      <FieldList projectKey={projectKey} canConfigure={canAdminister(access, projectKey)} canPromote={Boolean(access?.canAdministerOrg)} />
    </Page>
  );
}
