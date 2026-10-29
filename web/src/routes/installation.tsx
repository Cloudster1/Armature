import { useEffect, useState, type FormEvent } from "react";
import { Link, createRoute, useNavigate } from "@tanstack/react-router";
import { adminRoute } from "./admin";
import { useAccess } from "@/api/access";
import { ApiError } from "@/api/client";
import { useQueryClient } from "@tanstack/react-query";
import { useAddDemoProject, useAddSuiteDemo, useInstallation, useRenameInstallation, useSuiteDemo } from "@/api/installation";
import { Button, Card, ErrorBanner, Field, Page, PageHeader, SectionTitle, useToast } from "@/components/ui";
import { t } from "@/i18n";

export const installationRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/installation",
  component: InstallationPage,
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
          <Link to="/admin" className="hover:text-ink">
            {t.shell.admin.crumb}
          </Link>
        }
        title={t.access.installation.title}
      />
      {name !== undefined &&
        (canAdminister ? (
          <RenameForm name={name} />
        ) : (
          <Card className="flex items-center justify-between gap-3 px-5 py-3 text-sm">
            <span className="text-ink-muted">{t.access.newUser.name}</span>
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
      () => toast.success(t.access.users.renamed(wanted)),
      () => {},
    );
  }

  return (
    <Card className="p-5">
      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        {rename.error && !fieldErrors.name && <ErrorBanner>{(rename.error as Error).message}</ErrorBanner>}
        <Field
          label={t.access.newUser.name}
          id="field-installation-name"
          required
          value={typed}
          onChange={(event) => setTyped(event.target.value)}
          hint={t.access.installation.nameHint}
          error={fieldErrors.name}
        />
        <Button type="submit" loading={rename.isPending} disabled={!wanted || wanted === name}>
          {t.access.users.rename}
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
      <SectionTitle className="mb-2">{t.access.installation.demo}</SectionTitle>
      <Card className="space-y-3 p-5">
        {add.error && <ErrorBanner>{(add.error as Error).message}</ErrorBanner>}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="max-w-md text-sm text-ink-muted">{t.access.installation.demoDetail}</p>
          <Button variant="secondary" loading={add.isPending} data-action="add-demo-project" data-guide="add-demo-project" onClick={onAdd}>
            {t.access.installation.addDemo}
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
        <p className="max-w-md text-sm text-ink-muted">{t.access.installation.suiteDetail}</p>
        {data && (!demo || demo.state === "failed") && (
          <Button variant="secondary" loading={add.isPending} data-action="add-suite-demo" onClick={() => add.mutate()}>
            {t.access.installation.addSuite}
          </Button>
        )}
      </div>
      {building && demo && (
        <p role="status" className="text-sm text-ink">
          {t.access.installation.building(demo.step, demo.steps)}
        </p>
      )}
      {demo?.state === "done" && (
        <p className="text-sm text-ink">
          {t.access.installation.suiteIn}{" "}
          {demo.projects.map((key, i) => (
            <span key={key}>
              {i > 0 && (i === demo.projects.length - 1 ? t.access.installation.and : ", ")}
              <Link to="/projects/$projectKey" params={{ projectKey: key }} className="font-medium text-accent hover:underline">
                {key}
              </Link>
            </span>
          ))}
          {t.access.installation.asOf(demo.taken)}
        </p>
      )}
    </Card>
  );
}
