import { useEffect, useState, type FormEvent } from "react";
import { Link, createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useAccess } from "@/api/access";
import { ApiError } from "@/api/client";
import { useQueryClient } from "@tanstack/react-query";
import { useAddDemoProject, useAddSuiteDemo, useInstallation, useRenameInstallation, useSuiteDemo } from "@/api/installation";
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
    rename.mutateAsync(wanted).then(
      () => toast.success(`Renamed to ${wanted}`),
      () => {},
    );
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
    add.mutateAsync().then(
      ({ project }) => navigate({ to: "/projects/$projectKey", params: { projectKey: project.key } }),
      () => {},
    );
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
      <SuiteDemoCard />
    </section>
  );
}

/** The suite demo: the GitHub issues of Armature, Exciter and Stator, which their own suite demos link to. */
function SuiteDemoCard() {
  const { data } = useSuiteDemo(true);
  const add = useAddSuiteDemo();
  const queryClient = useQueryClient();
  const demo = data?.demo ?? null;
  const building = demo?.state === "queued" || demo?.state === "running";
  const [wasBuilding, setWasBuilding] = useState(false);
  const refused = add.error instanceof ApiError ? (add.error.fields.key ?? add.error.message) : (add.error as Error | null)?.message;

  // The new projects belong in every list once the worker is done.
  useEffect(() => {
    if (building) setWasBuilding(true);
    else if (wasBuilding && demo?.state === "done") {
      setWasBuilding(false);
      void queryClient.invalidateQueries();
    }
  }, [building, wasBuilding, demo?.state, queryClient]);

  return (
    <Card className="mt-3 space-y-3 p-5" data-suite-demo={data ? (demo?.state ?? "none") : "loading"}>
      {refused && <ErrorBanner>{refused}</ErrorBanner>}
      {demo?.state === "failed" && <ErrorBanner>{demo.message}</ErrorBanner>}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-md text-sm text-ink-muted">
          The issues Armature, Exciter and Stator were built from, taken from GitHub, each under its own number: Exciter's issue 261 is EXC-261. Exciter and
          Stator link their own suite demos to them.
        </p>
        {data && !demo && (
          <Button variant="secondary" loading={add.isPending} data-action="add-suite-demo" onClick={() => add.mutate()}>
            Add the suite demo
          </Button>
        )}
      </div>
      {building && demo && (
        <p role="status" className="text-sm text-ink">
          Building the suite demo: step {demo.step} of {demo.steps}.
        </p>
      )}
      {demo?.state === "done" && (
        <p className="text-sm text-ink">
          The suite demo is in{" "}
          {demo.projects.map((key, i) => (
            <span key={key}>
              {i > 0 && (i === demo.projects.length - 1 ? " and " : ", ")}
              <Link to="/projects/$projectKey" params={{ projectKey: key }} className="font-medium text-accent hover:underline">
                {key}
              </Link>
            </span>
          ))}
          , as GitHub had them on {demo.taken}.
        </p>
      )}
    </Card>
  );
}
