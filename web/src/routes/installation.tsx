import { useEffect, useState, type FormEvent } from "react";
import { Link, createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { ApiError } from "@/api/client";
import { useAddDemoProject, useInstallation, useRenameInstallation } from "@/api/installation";
import { Button, Card, ErrorBanner, Field, Page, PageHeader, SectionTitle, useToast } from "@/components/ui";

export const installationRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/installation",
  component: InstallationPage,
});

/** The page's old address, kept for bookmarks until the next release. */
export const organizationSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/organization",
  beforeLoad: () => {
    throw redirect({ to: "/settings/installation", replace: true });
  },
});

/** The installation itself: what everybody sees it called, and the demo an administrator may fill it with. */
function InstallationPage() {
  const { data } = useInstallation();
  const { data: access } = useAccess();
  const canAdminister = access?.canAdministerOrg ?? false;
  const name = data?.installation.name;

  return (
    <Page width="narrow">
      <PageHeader
        crumb={
          <Link to="/settings" className="hover:text-ink">
            Settings
          </Link>
        }
        title="Installation"
      />
      {name !== undefined &&
        (canAdminister ? (
          <RenameForm name={name} />
        ) : (
          <Card className="flex items-center justify-between gap-3 px-5 py-3 text-sm">
            <span className="text-ink-muted">Name</span>
            <span className="font-medium text-ink">{name}</span>
          </Card>
        ))}
      {canAdminister && <DemoProject />}
    </Page>
  );
}

function RenameForm({ name }: { name: string }) {
  const rename = useRenameInstallation();
  const toast = useToast();
  const [typed, setTyped] = useState(name);
  const fieldErrors = rename.error instanceof ApiError ? rename.error.fields : {};
  const wanted = typed.trim();

  useEffect(() => setTyped(name), [name]);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    rename.mutateAsync(wanted).then(() => toast.success(`Renamed to ${wanted}`), () => {});
  }

  return (
    <Card className="p-5">
      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        {rename.error && !fieldErrors.name && <ErrorBanner>{(rename.error as Error).message}</ErrorBanner>}
        <Field
          label="Name"
          id="field-installation-name"
          required
          value={typed}
          onChange={(event) => setTyped(event.target.value)}
          hint="Everybody sees it at the head of the sidebar, and invitations name it."
          error={fieldErrors.name}
        />
        <Button type="submit" loading={rename.isPending} disabled={!wanted || wanted === name}>
          Rename
        </Button>
      </form>
    </Card>
  );
}

function DemoProject() {
  const add = useAddDemoProject();
  const navigate = useNavigate();

  function onAdd() {
    add.mutateAsync().then(({ project }) => navigate({ to: "/projects/$projectKey", params: { projectKey: project.key } }), () => {});
  }

  return (
    <section className="mt-8">
      <SectionTitle className="mb-2">Demo</SectionTitle>
      <Card className="space-y-3 p-5">
        {add.error && <ErrorBanner>{(add.error as Error).message}</ErrorBanner>}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="max-w-md text-sm text-ink-muted">
            A made-up plant to show Armature with something in it: a production line and its maintenance, with issues, teams, boards, sprints and milestones, as
            two projects beside your own. You are their lead.
          </p>
          <Button variant="secondary" loading={add.isPending} data-action="add-demo-project" data-guide="add-demo-project" onClick={onAdd}>
            Add the demo project
          </Button>
        </div>
      </Card>
    </section>
  );
}
