import { useEffect } from "react";
import { Link, createRoute, redirect } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { meQueryKey } from "@/api/auth";
import { ApiError, request } from "@/api/client";
import { useConsent, useDecideConsent, type Consent } from "@/api/oauth";
import { Button, ErrorBanner } from "@/components/ui";
import { leave } from "@/lib/leave";

/**
 * Where a connected app sends a person to ask for their token. The page shows
 * what the api decided and sends the browser back to the app with the answer;
 * an app or a return address Armature does not know is never sent to.
 */
export const oauthAuthorizeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/oauth/authorize",
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData({ queryKey: meQueryKey, queryFn: () => request("/auth/me"), retry: false });
    } catch (error) {
      if (error instanceof ApiError && error.isUnauthenticated) {
        // The whole address, query and all, so the sign-in comes back to this request.
        throw redirect({ to: "/login", search: { next: location.href } });
      }
      throw error;
    }
  },
  component: ConsentPage,
});

/** The app's query, exactly as it sent it. */
function authorizeQuery(): string {
  return window.location.search.replace(/^\?/, "");
}

function ConsentPage() {
  const query = authorizeQuery();
  const { data, error, isLoading } = useConsent(query);
  const to = data?.redirectTo;
  useEffect(() => {
    if (to) leave(to);
  }, [to]);

  if (error) {
    const held = error instanceof ApiError && error.code === "second_factor_required";
    return (
      <AuthLayout title={held ? "Set up two-step sign-in first" : "This app cannot be connected"} footer={<BackHome />}>
        <p className="text-sm text-ink-muted" data-consent-refused>
          {held ? "Your account needs two-step sign-in before it connects anything. Set it up, then open the app's link again." : error.message}
        </p>
      </AuthLayout>
    );
  }
  if (isLoading || to || !data?.consent) {
    return (
      <AuthLayout title="Connecting" footer={<BackHome />}>
        <p className="text-sm text-ink-muted">One moment...</p>
      </AuthLayout>
    );
  }
  return <ConsentCard consent={data.consent} query={query} />;
}

function ConsentCard({ consent, query }: { consent: Consent; query: string }) {
  const decide = useDecideConsent();
  const { client } = consent;
  const answer = (decision: "allow" | "deny") => decide.mutate({ query, decision }, { onSuccess: (r) => leave(r.redirectTo) });

  return (
    <AuthLayout title={`Connect ${client.name}`} subtitle={client.clientUri} footer={<BackHome />}>
      <div className="space-y-4 text-sm text-ink" data-consent={client.name} data-consent-pending={consent.pending ? "true" : "false"}>
        {consent.pending && (
          <p data-consent-approve>
            <span className="font-semibold">{client.name}</span> at {consent.redirectHost} asks to be connected to Armature. Allowing it lets it ask anybody
            here to connect their account, with at most these rights.
          </p>
        )}
        <div>
          <p>
            <span className="font-semibold">{client.name}</span> wants to act as <span className="font-semibold">{consent.person}</span> in Armature:
          </p>
          <ul className="mt-2 list-disc space-y-1 pl-5 text-ink-muted">
            {consent.scopes.map((s) => (
              <li key={s.scope} data-consent-scope={s.scope}>
                {s.words}
              </li>
            ))}
          </ul>
        </div>
        <p className="text-ink-muted">Afterwards you go back to {consent.redirectHost}. Disconnect it any time under Access tokens, in your account menu.</p>
        {consent.refusal && (
          <p className="rounded-control border border-border bg-surface-raised px-3 py-2 text-ink-muted" data-consent-refusal>
            {consent.refusal}
          </p>
        )}
        {decide.error && <ErrorBanner>{(decide.error as Error).message}</ErrorBanner>}
        <div className="flex gap-2">
          {consent.mayAllow && (
            <Button loading={decide.isPending && decide.variables?.decision === "allow"} onClick={() => answer("allow")} data-action="consent-allow">
              Allow
            </Button>
          )}
          {consent.mayCancel && (
            <Button
              variant="ghost"
              loading={decide.isPending && decide.variables?.decision === "deny"}
              onClick={() => answer("deny")}
              data-action="consent-cancel"
            >
              Cancel
            </Button>
          )}
        </div>
      </div>
    </AuthLayout>
  );
}

function BackHome() {
  return (
    <Link to="/" className="font-medium text-accent hover:underline">
      Go to Armature
    </Link>
  );
}
