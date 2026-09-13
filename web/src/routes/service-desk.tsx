import { Fragment, useState, type FormEvent } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { LEVEL_STANDARD, useIssueTypes, type Priority } from "@/api/issues";
import { useTeams } from "@/api/teams";
import {
  humanDuration,
  useCreateRequestType,
  useDeleteRequestType,
  usePolicies,
  useRequestTypes,
  useUpdatePolicy,
  useUpdateRequestType,
  type Policy,
  type RequestType,
} from "@/api/desk";
import { Button, Card, ErrorBanner, Field, Page, PageHeader, SectionTitle, Select, Table, Td, Th } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { categoriesOf, groupByCategory } from "@/features/desk/templates";
import { BusinessHours, CannedResponses, Door, KnowledgeBase } from "@/features/desk/DeskExtras";
import { canAdminister, useAccess } from "@/api/access";

export const serviceDeskRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/service-desk",
  component: ServiceDeskPage,
});

const priorities: Priority[] = ["highest", "high", "medium", "low", "lowest"];

/** How the desk is set up: what customers can raise, and what was promised. */
function ServiceDeskPage() {
  const { projectKey } = serviceDeskRoute.useParams();
  const { data } = useProject(projectKey);
  const { data: access } = useAccess();

  return (
    <Page width="content" className="space-y-8">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Service desk"
      />
      <Door projectKey={projectKey} editable={canAdminister(access, projectKey)} />
      <RequestTypes projectKey={projectKey} />
      <Policies projectKey={projectKey} />
      <BusinessHours projectKey={projectKey} />
      <KnowledgeBase projectKey={projectKey} />
      <CannedResponses projectKey={projectKey} />
    </Page>
  );
}

function RequestTypes({ projectKey }: { projectKey: string }) {
  const { data } = useRequestTypes(projectKey);
  const { data: typeData } = useIssueTypes();
  const { data: teamData } = useTeams(projectKey);
  const create = useCreateRequestType(projectKey);
  const remove = useDeleteRequestType();
  const confirm = useConfirm();
  const [editing, setEditing] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [category, setCategory] = useState("");
  const [issueTypeId, setIssueTypeId] = useState("");
  const [priority, setPriority] = useState<Priority>("medium");
  const [teamId, setTeamId] = useState("");
  const [template, setTemplate] = useState("");

  const types = data?.requestTypes ?? [];
  const issueTypes = (typeData?.issueTypes ?? []).filter((t) => t.level >= LEVEL_STANDARD);
  const teams = teamData?.teams ?? [];
  const groups = groupByCategory(types);
  const categories = categoriesOf(types);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      {
        name: name.trim(),
        description: description.trim() || undefined,
        category: category.trim() || undefined,
        issueTypeId: issueTypeId || undefined,
        priority,
        teamId: teamId || undefined,
        detailsTemplate: template || undefined,
      },
      {
        onSuccess: () => {
          setName("");
          setDescription("");
          setTemplate("");
        },
      },
    );
  }

  return (
    <section>
      <SectionTitle className="mb-2">Request types</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">
        What a customer chooses from, under the category it belongs to. Each becomes an issue of a type, at a priority the desk decides, with a team it
        lands with and the details a requester starts from.
      </p>
      {types.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th className="w-32">Becomes</Th>
              <Th className="w-24">Priority</Th>
              <Th className="w-40">Team</Th>
              <Th className="w-32" aria-label="Actions" />
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => (
              <Fragment key={group.name}>
                <tr data-request-category={group.name}>
                  <Td colSpan={5} className="bg-surface-raised/60 text-xs font-medium tracking-wide text-ink-muted uppercase">
                    {group.name}
                  </Td>
                </tr>
                {group.types.map((rt) =>
                  editing === rt.id ? (
                    <EditRequestType key={rt.id} type={rt} issueTypes={issueTypes} teams={teams} onDone={() => setEditing("")} />
                  ) : (
                    <tr key={rt.id} data-request-type={rt.name}>
                      <Td>
                        <span className="text-ink">{rt.name}</span>
                        {rt.description && <span className="block text-sm text-ink-muted">{rt.description}</span>}
                      </Td>
                      <Td className="text-sm text-ink-muted">{rt.issueTypeName}</Td>
                      <Td className="text-sm text-ink-muted capitalize">{rt.priority}</Td>
                      <Td className="text-sm text-ink-muted" data-request-type-team={rt.teamName ?? ""}>
                        {rt.teamName ?? "Nobody in particular"}
                      </Td>
                      <Td className="text-right whitespace-nowrap">
                        <Button size="sm" variant="ghost" onClick={() => setEditing(rt.id)} data-request-type-edit={rt.name}>
                          Edit
                        </Button>
                        <Button size="sm" variant="ghost" loading={remove.isPending} onClick={async () => (await confirm({ noun: "request type", verb: "Remove", body: `${rt.name} is no longer offered; requests already raised through it keep their history.` })) && remove.mutate(rt.id)}>
                          Remove
                        </Button>
                      </Td>
                    </tr>
                  ),
                )}
              </Fragment>
            ))}
          </tbody>
        </Table>
      )}

      <Card className="mt-3 p-4">
        <form onSubmit={onSubmit} className="space-y-3" noValidate>
          {(create.error ?? remove.error) && <ErrorBanner>{((create.error ?? remove.error) as Error).message}</ErrorBanner>}
          <div className="grid gap-3 sm:grid-cols-[1fr_1fr_1fr]">
            <Field label="Request type" value={name} onChange={(e) => setName(e.target.value)} placeholder="Laptop replacement" />
            <Field label="Description" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="A device that has stopped working." />
            <Field label="Category" value={category} onChange={(e) => setCategory(e.target.value)} placeholder="Hardware" list="rt-categories" />
            <datalist id="rt-categories">
              {categories.map((c) => (
                <option key={c} value={c} />
              ))}
            </datalist>
          </div>
          <div className="grid gap-3 sm:grid-cols-[1fr_1fr_1fr]">
            <Select id="rt-issue-type" label="Becomes" value={issueTypeId} onChange={(e) => setIssueTypeId(e.target.value)}>
              <option value="">The first issue type</option>
              {issueTypes.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </Select>
            <Select id="rt-priority" label="At priority" value={priority} onChange={(e) => setPriority(e.target.value as Priority)}>
              {priorities.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </Select>
            <Select id="rt-team" label="Lands with" value={teamId} onChange={(e) => setTeamId(e.target.value)}>
              <option value="">Nobody in particular</option>
              {teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </Select>
          </div>
          <Field
            label="Details template"
            id="rt-template"
            rows={3}
            value={template}
            onChange={(e) => setTemplate(e.target.value)}
            placeholder={"Device:\nWhat happened:\nSince when:"}
            data-request-type-template
            className="font-mono"
          />
          <div className="flex items-end">
            <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
              Add request type
            </Button>
          </div>
        </form>
      </Card>
    </section>
  );
}

/** One request type turned into a form in place, saved with what changed. */
function EditRequestType({
  type,
  issueTypes,
  teams,
  onDone,
}: {
  type: RequestType;
  issueTypes: Array<{ id: string; name: string }>;
  teams: Array<{ id: string; name: string }>;
  onDone: () => void;
}) {
  const update = useUpdateRequestType();
  const [name, setName] = useState(type.name);
  const [description, setDescription] = useState(type.description ?? "");
  const [category, setCategory] = useState(type.category);
  const [issueTypeId, setIssueTypeId] = useState(type.issueTypeId);
  const [priority, setPriority] = useState<Priority>(type.priority);
  const [teamId, setTeamId] = useState(type.teamId ?? "");
  const [template, setTemplate] = useState(type.detailsTemplate);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    update.mutate(
      {
        id: type.id,
        name: name.trim(),
        description: description.trim(),
        category: category.trim(),
        issueTypeId,
        priority,
        detailsTemplate: template,
        teamId: teamId || undefined,
        clearTeam: !teamId,
      },
      { onSuccess: onDone },
    );
  }

  return (
    <tr data-request-type={type.name} data-request-type-editing>
      <Td colSpan={5}>
        <form onSubmit={onSubmit} className="space-y-3 py-1" noValidate>
          {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
          <div className="grid gap-3 sm:grid-cols-[1fr_1fr_1fr]">
            <Field label="Request type" id={`edit-name-${type.id}`} value={name} onChange={(e) => setName(e.target.value)} />
            <Field label="Description" id={`edit-description-${type.id}`} value={description} onChange={(e) => setDescription(e.target.value)} />
            <Field label="Category" id={`edit-category-${type.id}`} value={category} onChange={(e) => setCategory(e.target.value)} list="rt-categories" />
          </div>
          <div className="grid gap-3 sm:grid-cols-[1fr_1fr_1fr]">
            <Select id={`edit-type-${type.id}`} label="Becomes" value={issueTypeId} onChange={(e) => setIssueTypeId(e.target.value)}>
              {issueTypes.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </Select>
            <Select id={`edit-priority-${type.id}`} label="At priority" value={priority} onChange={(e) => setPriority(e.target.value as Priority)}>
              {priorities.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </Select>
            <Select id={`edit-team-${type.id}`} label="Lands with" value={teamId} onChange={(e) => setTeamId(e.target.value)}>
              <option value="">Nobody in particular</option>
              {teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </Select>
          </div>
          <Field label="Details template" id={`edit-template-${type.id}`} rows={3} value={template} onChange={(e) => setTemplate(e.target.value)} className="font-mono" />
          <div className="flex gap-2">
            <Button type="submit" size="sm" loading={update.isPending} disabled={!name.trim()}>
              Save request type
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={onDone}>
              Cancel
            </Button>
          </div>
        </form>
      </Td>
    </tr>
  );
}

function Policies({ projectKey }: { projectKey: string }) {
  const { data } = usePolicies(projectKey);
  const policies = data?.policies ?? [];
  return (
    <section>
      <SectionTitle className="mb-2">Goals</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">
        Hours the desk has, by priority. The clocks stop while a request waits on the customer. A request keeps the goal it started with.
      </p>
      <div className="space-y-3">
        {policies.map((p) => (
          <PolicyEditor key={p.id} policy={p} />
        ))}
      </div>
    </section>
  );
}

function PolicyEditor({ policy }: { policy: Policy }) {
  const update = useUpdatePolicy();
  const [hours, setHours] = useState<Record<Priority, string>>(() =>
    Object.fromEntries(priorities.map((p) => [p, policy.goals[p] ? String(policy.goals[p]! / 60) : ""])) as Record<Priority, string>,
  );

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    const goals: Partial<Record<Priority, number>> = {};
    for (const p of priorities) {
      const value = Number(hours[p]);
      goals[p] = value > 0 ? Math.round(value * 60) : 0;
    }
    update.mutate({ id: policy.id, goals });
  }

  return (
    <Card className="p-4" data-policy={policy.metric}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <div className="flex items-baseline justify-between">
          <h3 className="text-sm font-medium text-ink">{policy.name}</h3>
          <span className="text-xs text-ink-subtle">
            {policy.pauseStatuses.length > 0 ? `pauses in ${policy.pauseStatuses.join(", ")}` : "never pauses"}
          </span>
        </div>
        <div className="grid grid-cols-5 gap-2">
          {priorities.map((p) => (
            <Field
              key={p}
              id={`${policy.metric}-${p}`}
              label={p}
              type="number"
              min={0}
              step={0.5}
              value={hours[p]}
              onChange={(e) => setHours({ ...hours, [p]: e.target.value })}
              className="capitalize"
            />
          ))}
        </div>
        <div className="flex items-center gap-3">
          <Button type="submit" size="sm" variant="secondary" loading={update.isPending}>
            Save goals
          </Button>
          <span className="text-xs text-ink-subtle">
            {priorities.map((p) => (policy.goals[p] ? `${p} ${humanDuration(policy.goals[p]! * 60)}` : null)).filter(Boolean).join(" · ")}
          </span>
        </div>
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      </form>
    </Card>
  );
}
