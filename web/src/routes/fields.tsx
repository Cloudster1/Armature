import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { Page, PageHeader } from "@/components/ui";
import { FieldList } from "@/features/fields/FieldList";

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
        title="Fields"
        meta="What this project records about an issue beyond the standard fields."
      />
      <FieldList projectKey={projectKey} canConfigure={canAdminister(access, projectKey)} canPromote={Boolean(access?.canAdministerOrg)} />
    </Page>
  );
}
