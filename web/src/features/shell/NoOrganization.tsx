import { useNavigate } from "@tanstack/react-router";
import { useLogout, useMe, useSwitchOrg } from "@/api/auth";
import { Button, Card, ErrorBanner, Wordmark } from "@/components/ui";

/**
 * A session that points at no organization, offering the ones the person is
 * still in. Every page of the shell needs one, so the shell is not shown.
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
        <Wordmark />
        <h1 className="mt-3 text-xl font-semibold tracking-tight text-ink">
          Choose an organization
        </h1>
        <p className="mt-1 text-sm text-ink-muted">
          The organization you were working in is no longer open to you.
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
            variant="ghost"
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
