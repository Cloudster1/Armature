import { useNavigate } from "@tanstack/react-router";
import { useLogout, useMe, useSwitchOrg } from "@/api/auth";
import { Button, Card, ErrorBanner } from "@/components/ui";

/**
 * Somebody signed in whose session points at no organization: they were let go
 * from it, or it is gone. Every page of the shell needs one, so showing the
 * shell would only be a row of empty tables that never says why.
 */
export function NoOrganization() {
  const { data } = useMe();
  const switchOrg = useSwitchOrg();
  const logout = useLogout();
  const navigate = useNavigate();
  const organizations = data?.organizations ?? [];

  return (
    <div
      className="flex min-h-full items-center justify-center px-4 py-12"
      data-no-organization
    >
      <div className="w-full max-w-sm">
        <p className="font-mono text-sm font-medium tracking-wide text-ink-muted">
          armature
        </p>
        <h1 className="mt-3 text-xl font-semibold tracking-tight text-ink">
          {organizations.length > 0
            ? "Choose an organization"
            : "You are not in an organization"}
        </h1>
        <p className="mt-1 text-sm text-ink-muted">
          {organizations.length > 0
            ? "The organization you were working in is no longer open to you."
            : "You were removed from your organization, or it no longer exists. Ask one of its administrators to invite you again."}
        </p>

        <Card className="mt-6 space-y-3 p-5">
          {switchOrg.error && (
            <ErrorBanner>{(switchOrg.error as Error).message}</ErrorBanner>
          )}
          {organizations.map((org) => (
            <Button
              key={org.orgId}
              variant="secondary"
              className="w-full justify-start"
              loading={
                switchOrg.isPending && switchOrg.variables === org.orgSlug
              }
              data-org-choice={org.orgSlug}
              onClick={() => switchOrg.mutate(org.orgSlug)}
            >
              {org.orgName}
            </Button>
          ))}
          <Button
            variant={organizations.length > 0 ? "ghost" : "primary"}
            className="w-full"
            loading={logout.isPending}
            data-action="sign-out"
            onClick={() =>
              logout.mutate(undefined, {
                onSuccess: () => navigate({ to: "/login" }),
              })
            }
          >
            Sign out
          </Button>
        </Card>
      </div>
    </div>
  );
}
