import { createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { Page } from "@/components/ui";
import { IssuePage } from "@/features/issues/IssuePage";

export const issueDetailRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/issues/$issueKey",
  component: IssueDetail,
});

function IssueDetail() {
  const { issueKey } = issueDetailRoute.useParams();
  return (
    <Page width="content">
      <IssuePage issueKey={issueKey} />
    </Page>
  );
}
