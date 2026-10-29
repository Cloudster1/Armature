import { Fragment, useState, type FormEvent } from "react";
import { createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { LEVEL_STANDARD, type Priority } from "@/api/issues";
import { useProjectIssueTypes } from "@/api/issueTypes";
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
import { categoriesOf, categoryHeading, groupByCategory } from "@/features/desk/templates";
import { BusinessHours, CannedResponses, Door, KnowledgeBase } from "@/features/desk/DeskExtras";
import { Companies } from "@/features/desk/Companies";
import { canAdminister, useAccess } from "@/api/access";
import { t } from "@/i18n";

export const serviceDeskRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/service-desk",
  component: ServiceDeskPage,
});

const priorities: Priority[] = ["highest", "high", "medium", "low", "lowest"];

/** How the desk is set up: what customers can raise, and what was promised. */
function ServiceDeskPage() {
  const { projectKey } = serviceDeskRoute.useParams();
  const { data: access } = useAccess();

  return (
    <Page width="content" className="space-y-8">
      <PageHeader title={t.desk.setup.title} />
      <Door projectKey={projectKey} editable={canAdminister(access, projectKey)} />
      <RequestTypes projectKey={projectKey} />
      <Companies projectKey={projectKey} editable={canAdminister(access, projectKey)} />
      <Policies projectKey={projectKey} />
      <BusinessHours projectKey={projectKey} />
      <KnowledgeBase projectKey={projectKey} />
      <CannedResponses projectKey={projectKey} />
    </Page>
  );
}

export function RequestTypes({ projectKey }: { projectKey: string }) {
  const { data } = useRequestTypes(projectKey);
  const { data: typeData } = useProjectIssueTypes(projectKey);
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
  const issueTypes = (typeData?.issueTypes ?? []).filter((it) => it.level >= LEVEL_STANDARD);
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
      <SectionTitle className="mb-2">{t.desk.setup.requestTypes}</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">{t.desk.setup.requestTypesIntro}</p>
      {types.length > 0 && (
        <Table>
          <thead>
            <tr>
              <Th>{t.desk.setup.name}</Th>
              <Th className="w-32">{t.desk.setup.becomes}</Th>
              <Th className="w-24">{t.desk.setup.priority}</Th>
              <Th className="w-40">{t.desk.setup.team}</Th>
              <Th className="w-32" aria-label={t.desk.setup.actions} />
            </tr>
          </thead>
          <tbody>
            {groups.map((group) => (
              <Fragment key={group.name}>
                <tr data-request-category={group.name}>
                  <Td colSpan={5} className="bg-surface-raised/60 text-xs font-medium tracking-wide text-ink-muted uppercase">
                    {categoryHeading(group.name)}
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
                      <Td className="text-sm text-ink-muted capitalize">{t.issueList.priority.options[rt.priority] ?? rt.priority}</Td>
                      <Td className="text-sm text-ink-muted" data-request-type-team={rt.teamName ?? ""}>
                        {rt.teamName ?? t.desk.setup.nobodyInParticular}
                      </Td>
                      <Td className="text-right whitespace-nowrap">
                        <Button size="sm" variant="ghost" onClick={() => setEditing(rt.id)} data-request-type-edit={rt.name}>
                          {t.desk.setup.edit}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          loading={remove.isPending}
                          onClick={async () =>
                            (await confirm({
                              noun: t.kit.confirm.things.requestType,
                              verb: t.kit.confirm.verbs.remove,
                              body: t.desk.setup.removeBody(rt.name),
                            })) && remove.mutate(rt.id)
                          }
                        >
                          {t.desk.setup.remove}
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
            <Field label={t.desk.setup.requestType} value={name} onChange={(e) => setName(e.target.value)} placeholder={t.desk.setup.typePlaceholder} />
            <Field
              label={t.desk.setup.description}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t.desk.setup.descriptionPlaceholder}
            />
            <Field
              label={t.desk.setup.category}
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              placeholder={t.desk.setup.categoryPlaceholder}
              list="rt-categories"
            />
            <datalist id="rt-categories">
              {categories.map((c) => (
                <option key={c} value={c} />
              ))}
            </datalist>
          </div>
          <div className="grid gap-3 sm:grid-cols-[1fr_1fr_1fr]">
            <Select id="rt-issue-type" label={t.desk.setup.becomes} value={issueTypeId} onChange={(e) => setIssueTypeId(e.target.value)}>
              <option value="">{t.desk.setup.firstIssueType}</option>
              {issueTypes.map((it) => (
                <option key={it.id} value={it.id}>
                  {it.name}
                </option>
              ))}
            </Select>
            <Select id="rt-priority" label={t.desk.setup.atPriority} value={priority} onChange={(e) => setPriority(e.target.value as Priority)}>
              {priorities.map((p) => (
                <option key={p} value={p}>
                  {t.issueList.priority.options[p]}
                </option>
              ))}
            </Select>
            <Select id="rt-team" label={t.desk.setup.landsWith} value={teamId} onChange={(e) => setTeamId(e.target.value)}>
              <option value="">{t.desk.setup.nobodyInParticular}</option>
              {teams.map((team) => (
                <option key={team.id} value={team.id}>
                  {team.name}
                </option>
              ))}
            </Select>
          </div>
          <Field
            label={t.desk.setup.detailsTemplate}
            id="rt-template"
            rows={3}
            value={template}
            onChange={(e) => setTemplate(e.target.value)}
            placeholder={t.desk.setup.templatePlaceholder}
            data-request-type-template
            className="font-mono"
          />
          <div className="flex items-end">
            <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
              {t.desk.setup.add}
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
            <Field label={t.desk.setup.requestType} id={`edit-name-${type.id}`} value={name} onChange={(e) => setName(e.target.value)} />
            <Field label={t.desk.setup.description} id={`edit-description-${type.id}`} value={description} onChange={(e) => setDescription(e.target.value)} />
            <Field
              label={t.desk.setup.category}
              id={`edit-category-${type.id}`}
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              list="rt-categories"
            />
          </div>
          <div className="grid gap-3 sm:grid-cols-[1fr_1fr_1fr]">
            <Select id={`edit-type-${type.id}`} label={t.desk.setup.becomes} value={issueTypeId} onChange={(e) => setIssueTypeId(e.target.value)}>
              {issueTypes.map((it) => (
                <option key={it.id} value={it.id}>
                  {it.name}
                </option>
              ))}
            </Select>
            <Select id={`edit-priority-${type.id}`} label={t.desk.setup.atPriority} value={priority} onChange={(e) => setPriority(e.target.value as Priority)}>
              {priorities.map((p) => (
                <option key={p} value={p}>
                  {t.issueList.priority.options[p]}
                </option>
              ))}
            </Select>
            <Select id={`edit-team-${type.id}`} label={t.desk.setup.landsWith} value={teamId} onChange={(e) => setTeamId(e.target.value)}>
              <option value="">{t.desk.setup.nobodyInParticular}</option>
              {teams.map((team) => (
                <option key={team.id} value={team.id}>
                  {team.name}
                </option>
              ))}
            </Select>
          </div>
          <Field
            label={t.desk.setup.detailsTemplate}
            id={`edit-template-${type.id}`}
            rows={3}
            value={template}
            onChange={(e) => setTemplate(e.target.value)}
            className="font-mono"
          />
          <div className="flex gap-2">
            <Button type="submit" size="sm" loading={update.isPending} disabled={!name.trim()}>
              {t.desk.setup.save}
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={onDone}>
              {t.kit.cancel}
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
      <SectionTitle className="mb-2">{t.desk.setup.goals}</SectionTitle>
      <p className="mb-3 text-sm text-ink-muted">{t.desk.setup.goalsIntro}</p>
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
  const [hours, setHours] = useState<Record<Priority, string>>(
    () => Object.fromEntries(priorities.map((p) => [p, policy.goals[p] ? String(policy.goals[p]! / 60) : ""])) as Record<Priority, string>,
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
            {policy.pauseStatuses.length > 0 ? t.desk.setup.pausesIn(policy.pauseStatuses.join(", ")) : t.desk.setup.neverPauses}
          </span>
        </div>
        <div className="grid grid-cols-5 gap-2">
          {priorities.map((p) => (
            <Field
              key={p}
              id={`${policy.metric}-${p}`}
              label={t.issueList.priority.options[p]!}
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
            {t.desk.setup.saveGoals}
          </Button>
          <span className="text-xs text-ink-subtle">
            {priorities
              .map((p) => (policy.goals[p] ? `${t.issueList.priority.options[p]} ${humanDuration(policy.goals[p]! * 60)}` : null))
              .filter(Boolean)
              .join(" · ")}
          </span>
        </div>
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      </form>
    </Card>
  );
}
