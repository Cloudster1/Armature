import { useState } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useDeleteOrganization, useMe } from "@/api/auth";
import { Button, Card, Dialog, ErrorBanner, Field, Page, PageHeader, SectionTitle } from "@/components/ui";

export const organizationRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/organization",
  component: OrganizationPage,
});

function OrganizationPage() {
  const { data } = useMe();
  const org = data?.principal?.org;
  const owner = data?.principal?.role === "owner";
  const [deleting, setDeleting] = useState(false);

  if (!org) return null;

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Organization"
      />

      <Card className="divide-y divide-border">
        <div className="flex items-center justify-between gap-3 px-5 py-3 text-sm">
          <span className="text-ink-muted">Name</span>
          <span className="font-medium text-ink">{org.name}</span>
        </div>
        <div className="flex items-center justify-between gap-3 px-5 py-3 text-sm">
          <span className="text-ink-muted">Address</span>
          <code className="font-mono text-ink">{org.slug}</code>
        </div>
      </Card>

      <section className="mt-8">
        <SectionTitle className="mb-2">Delete the organization</SectionTitle>
        <Card className="flex flex-wrap items-center justify-between gap-3 p-5">
          <p className="max-w-md text-sm text-ink-muted">
            {owner
              ? "Every project, issue, comment, attachment and the audit log go, for everybody in it, and cannot be brought back. The people keep their accounts for their other organizations."
              : "Only an owner of the organization can delete it."}
          </p>
          {owner && (
            <Button variant="danger" data-action="delete-organization" onClick={() => setDeleting(true)}>
              Delete organization
            </Button>
          )}
        </Card>
      </section>

      {deleting && <DeleteOrganizationDialog name={org.name} slug={org.slug} onClose={() => setDeleting(false)} />}
    </Page>
  );
}

/**
 * Typing the address back is the confirmation, not a yes button: this is the
 * one delete that cannot be undone and takes other people's work with it.
 */
function DeleteOrganizationDialog({ name, slug, onClose }: { name: string; slug: string; onClose: () => void }) {
  const remove = useDeleteOrganization();
  const navigate = useNavigate();
  const [typed, setTyped] = useState("");

  return (
    <Dialog
      open
      onClose={onClose}
      title={`Delete ${name}?`}
      description="This cannot be undone."
      attrs={{ "data-delete-organization": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Keep it
          </Button>
          <Button
            variant="danger"
            disabled={typed !== slug}
            loading={remove.isPending}
            data-action="confirm-delete-organization"
            onClick={() => remove.mutateAsync(typed).then(() => navigate({ to: "/" }), () => {})}
          >
            Delete everything
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {remove.error && <ErrorBanner>{(remove.error as Error).message}</ErrorBanner>}
        <Field label={`Type ${slug} to confirm`} id="field-confirm-organization" autoComplete="off" value={typed} onChange={(event) => setTyped(event.target.value)} />
      </div>
    </Dialog>
  );
}
