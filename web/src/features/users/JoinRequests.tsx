import { useAdmitJoinRequest, useDeclineJoinRequest, useJoinRequests, type JoinRequest } from "@/api/users";
import { Button, Card, ErrorBanner, useToast } from "@/components/ui";
import { useFormat } from "@/lib/format";
import { t } from "@/i18n";

/**
 * People the identity provider vouched for who found no membership here.
 * Letting one in is the whole of an invitation, minus the mail: they sign in
 * again and are through.
 */
export function JoinRequests() {
  const { data } = useJoinRequests();
  const admit = useAdmitJoinRequest();
  const decline = useDeclineJoinRequest();
  const toast = useToast();
  const format = useFormat();
  const requests = data?.requests ?? [];
  if (requests.length === 0) return null;
  const failure = (admit.error ?? decline.error) as Error | null;

  function letIn(request: JoinRequest) {
    admit.mutate({ userId: request.userId, role: "member" }, { onSuccess: () => toast.success(t.access.join.letIn(request.name)) });
  }

  return (
    <Card className="space-y-2 p-4" data-join-requests="">
      <h2 className="text-sm font-semibold text-ink">{t.access.join.title}</h2>
      <p className="text-sm text-ink-muted">{t.access.join.detail}</p>
      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      <ul className="divide-y divide-border">
        {requests.map((request) => (
          <li key={request.userId} className="flex items-center gap-3 py-2" data-join-request={request.email}>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm text-ink">{request.name || request.email}</div>
              <div className="truncate text-xs text-ink-subtle">{t.access.join.asked(request.email, format.dateTime(request.requestedAt))}</div>
            </div>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => decline.mutate({ userId: request.userId })}
              loading={decline.isPending}
              data-action="decline-request"
            >
              {t.access.join.turnAway}
            </Button>
            <Button size="sm" onClick={() => letIn(request)} loading={admit.isPending} data-action="admit-request">
              {t.access.join.admit}
            </Button>
          </li>
        ))}
      </ul>
    </Card>
  );
}
