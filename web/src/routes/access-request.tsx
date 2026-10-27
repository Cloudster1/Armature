import { Link, createRoute } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccessRequest, useGrantAccessRequest } from "@/api/accessRequests";
import { Button, Card, EmptyState, ErrorBanner, Page, PageHeader, Skeleton } from "@/components/ui";
import { useFormat } from "@/lib/format";
import { t } from "@/i18n";

/** Where an administrator reads a request for access and grants it in one click. */
export const accessRequestRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/access-requests/$requestId",
  component: AccessRequestPage,
});

function AccessRequestPage() {
  const { requestId } = accessRequestRoute.useParams();
  const { data, isLoading, error } = useAccessRequest(requestId);
  const grant = useGrantAccessRequest(requestId);
  const format = useFormat();
  const words = t.issueView.accessRequest;

  if (isLoading) return <Skeleton lines={4} />;
  const request = data?.accessRequest;
  if (error || !request) {
    return (
      <Page width="content">
        <EmptyState title={words.title} description={words.missing} />
      </Page>
    );
  }
  const who = request.requester.name || request.requester.email;
  return (
    <Page width="content">
      <PageHeader title={words.title} meta={format.dateTime(request.createdAt)} />
      <Card className="p-5" data-access-request={request.issueKey} data-access-request-state={request.grantedAt ? "granted" : "open"}>
        <p className="font-medium text-ink">{words.asks(who, request.issueKey)}</p>
        <p className="text-sm text-ink-muted">{request.requester.email}</p>
        <blockquote className="mt-3 border-l-2 border-border pl-3 text-sm text-ink">{request.message || words.noMessage}</blockquote>
        <div className="mt-4">
          {grant.error && <ErrorBanner>{(grant.error as Error).message}</ErrorBanner>}
          {request.grantedAt ? (
            <p className="text-sm text-ink" role="status">
              {words.granted(who, request.projectName)}{" "}
              {request.grantedBy && words.grantedBy(request.grantedBy.name || request.grantedBy.email, format.dateTime(request.grantedAt))}{" "}
              <Link to="/issues/$issueKey" params={{ issueKey: request.issueKey }} className="text-accent hover:underline">
                {request.issueKey}
              </Link>
            </p>
          ) : (
            <Button onClick={() => grant.mutate()} loading={grant.isPending} data-action="grant-access">
              {words.grant(who, request.projectName)}
            </Button>
          )}
        </div>
      </Card>
    </Page>
  );
}
