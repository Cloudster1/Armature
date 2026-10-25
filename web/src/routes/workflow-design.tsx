import { createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import type { Workflow } from "@/api/workflows";
import { WorkflowDesigner } from "@/features/workflows/designer/WorkflowDesigner";
import { PageHeader } from "@/components/ui";
import { t } from "@/i18n";

/** A page of its own: the canvas wants the width the settings list does not. */
export const workflowNewRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/workflows/new",
  component: () => <DesignerPage />,
});

export const workflowDesignRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/workflows/$workflowId/design",
  component: DesignExistingPage,
});

function DesignExistingPage() {
  const { workflowId } = workflowDesignRoute.useParams();
  return <DesignerPage workflowId={workflowId} />;
}

function DesignerPage({ workflowId }: { workflowId?: string }) {
  const navigate = useNavigate();
  const back = (saved?: Workflow) => navigate({ to: "/settings/workflows", search: { workflow: saved?.id, q: saved?.name } });

  return (
    <div>
      <PageHeader title={workflowId ? t.workflows.designer.design : t.workflows.designer.new} />
      <WorkflowDesigner workflowId={workflowId} onDone={back} onCancel={() => back()} />
    </div>
  );
}
