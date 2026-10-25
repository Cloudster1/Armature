import { useEffect } from "react";
import { Link, createRoute, redirect } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { AuthLayout } from "./auth";
import { meQueryKey } from "@/api/auth";
import { ApiError, request } from "@/api/client";
import { useConsent, useDecideConsent, type Consent } from "@/api/oauth";
import { Button, ErrorBanner } from "@/components/ui";
import { leave } from "@/lib/leave";
import { t } from "@/i18n";

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
      <AuthLayout title={held ? t.doors.consent.secondFactorFirst : t.doors.consent.cannotConnect} footer={<BackHome />}>
        <p className="text-sm text-ink-muted" data-consent-refused>
          {held ? t.doors.consent.secondFactorBody : error.message}
        </p>
      </AuthLayout>
    );
  }
  if (isLoading || to || !data?.consent) {
    return (
      <AuthLayout title={t.doors.consent.connecting} footer={<BackHome />}>
        <p className="text-sm text-ink-muted">{t.doors.consent.oneMoment}</p>
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
    <AuthLayout title={t.doors.consent.connect(client.name)} subtitle={client.clientUri} footer={<BackHome />}>
      <div className="space-y-4 text-sm text-ink" data-consent={client.name} data-consent-pending={consent.pending ? "true" : "false"}>
        {consent.pending && (
          <p data-consent-approve>
            <span className="font-semibold">{client.name}</span>
            {t.doors.consent.asks(consent.redirectHost)}
          </p>
        )}
        <div>
          <p>
            <span className="font-semibold">{client.name}</span>
            {t.doors.consent.wantsToActAs}
            <span className="font-semibold">{consent.person}</span>
            {t.doors.consent.inArmature}
          </p>
          <ul className="mt-2 list-disc space-y-1 pl-5 text-ink-muted">
            {consent.scopes.map((s) => (
              <li key={s.scope} data-consent-scope={s.scope}>
                {s.words}
              </li>
            ))}
          </ul>
        </div>
        <p className="text-ink-muted">{t.doors.consent.afterwards(consent.redirectHost)}</p>
        {consent.refusal && (
          <p className="rounded-control border border-border bg-surface-raised px-3 py-2 text-ink-muted" data-consent-refusal>
            {consent.refusal}
          </p>
        )}
        {decide.error && <ErrorBanner>{(decide.error as Error).message}</ErrorBanner>}
        <div className="flex gap-2">
          {consent.mayAllow && (
            <Button loading={decide.isPending && decide.variables?.decision === "allow"} onClick={() => answer("allow")} data-action="consent-allow">
              {t.doors.consent.allow}
            </Button>
          )}
          {consent.mayCancel && (
            <Button
              variant="ghost"
              loading={decide.isPending && decide.variables?.decision === "deny"}
              onClick={() => answer("deny")}
              data-action="consent-cancel"
            >
              {t.kit.cancel}
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
      {t.doors.consent.goToArmature}
    </Link>
  );
}
