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
      {/* Keyed by issue, so following a link to another issue carries no draft along. */}
      <IssuePage key={issueKey} issueKey={issueKey} />
    </Page>
  );
}
