import { useEffect, useState } from "react";
import { permissionWords, useAssignments, useGrantRole, useGroups, useRevokeRole, useRoleName, useRoles, type Assignment, type Role } from "@/api/access";
import { Button, Card, clampOffset, cx, EmptyState, ErrorBanner, ListSearch, Pager, Select, SelectInput, shownOf, Table, Td, Th } from "@/components/ui";
import { ROLE_ASSIGNMENT_PAGE_SIZE } from "@/config";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { ProjectPicker } from "@/features/projects/ProjectPicker";
import { PersonPicker } from "@/features/people/PersonPicker";
import { t } from "@/i18n";

/**
 * Who holds what.
 *
 * A grant is a role, a scope and a subject: a person or a group, over the whole
 * organization or over one project. Reading the table should answer "why can
 * this person do that" without anybody having to know the schema.
 */
export function RoleTable({ projectKey }: { projectKey?: string }) {
  // Held here rather than in the address, like the tab itself: the page is the tab's own.
  const [q, setQ] = useState("");
  const [role, setRole] = useState<Role | "">("");
  const [offset, setOffset] = useState(0);
  const { data, isLoading, error } = useAssignments({ projectKey, q, role, offset });
  const { data: roleData } = useRoles();
  const revoke = useRevokeRole();
  const confirm = useConfirm();
  const roleName = useRoleName();

  const assignments = data?.assignments ?? [];
  const failure = (error ?? revoke.error) as Error | undefined;
  const narrowed = Boolean(q || role);

  // Grants leave the list while it is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) setOffset(clampOffset(offset, data.total, ROLE_ASSIGNMENT_PAGE_SIZE));
  }, [data, offset]);

  return (
    <div className="space-y-4">
      <GrantForm projectKey={projectKey} />

      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}

      <div className="flex flex-wrap items-center gap-3">
        <ListSearch
          aria-label={t.access.grants.find}
          placeholder={t.access.grants.findPlaceholder}
          value={q}
          onChange={(text) => {
            setQ(text);
            setOffset(0);
          }}
          className="w-72"
          data-assignment-search=""
        />
        <SelectInput
          aria-label={t.access.grants.onlyRole}
          controlSize="sm"
          value={role}
          onChange={(event) => {
            setRole(event.target.value);
            setOffset(0);
          }}
        >
          <option value="">{t.access.grants.everyRole}</option>
          {(roleData?.roles ?? []).map((each) => (
            <option key={each.role} value={each.role}>
              {each.name}
            </option>
          ))}
        </SelectInput>
        {data && <span className="text-sm text-ink-muted tabular-nums">{shownOf(data.total, data.offset, assignments.length, t.kit.counted.grant)}</span>}
      </div>

      {isLoading && <p className="text-sm text-ink-muted">{t.access.loading}</p>}

      {!isLoading && assignments.length === 0 && narrowed ? (
        <EmptyState title={t.access.grants.noMatch} description={t.access.grants.noMatchDetail} />
      ) : !isLoading && assignments.length === 0 ? (
        <EmptyState title={t.access.grants.empty} description={t.access.grants.emptyDetail} />
      ) : (
        <Table data-testid="role-assignments">
          <thead>
            <tr>
              <Th>{t.access.grants.who}</Th>
              <Th>{t.access.grants.role}</Th>
              <Th>{t.access.grants.where}</Th>
              <Th>
                <span className="sr-only">{t.access.actions}</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {assignments.map((assignment) => (
              <AssignmentRow
                key={assignment.id}
                assignment={assignment}
                onRevoke={async () =>
                  (await confirm({
                    noun: t.kit.confirm.things.role,
                    verb: t.kit.confirm.verbs.revoke,
                    body: t.access.grants.revokeBody(roleName(assignment.role)),
                  })) && revoke.mutate(assignment.id)
                }
              />
            ))}
          </tbody>
        </Table>
      )}
      {data && <Pager label={t.access.grants.pages} total={data.total} offset={data.offset} pageSize={ROLE_ASSIGNMENT_PAGE_SIZE} onChange={setOffset} />}
    </div>
  );
}

function AssignmentRow({ assignment, onRevoke }: { assignment: Assignment; onRevoke: () => void }) {
  const toGroup = Boolean(assignment.groupId);
  const roleName = useRoleName();
  return (
    <tr data-assignment={assignment.id}>
      <Td className="w-full min-w-48">
        <span className="text-ink">{assignment.groupName || assignment.userName}</span>
        {toGroup && <span className="ml-2 rounded-full bg-surface-raised px-2 py-0.5 text-2xs text-ink-muted">{t.access.grants.group}</span>}
      </Td>
      <Td className="whitespace-nowrap text-ink">{roleName(assignment.role)}</Td>
      <Td className="whitespace-nowrap text-ink-muted">{assignment.projectKey || t.access.grants.everyProject}</Td>
      <Td className="text-right">
        <Button size="sm" variant="ghost" onClick={onRevoke}>
          {t.access.grants.revoke}
        </Button>
      </Td>
    </tr>
  );
}

function GrantForm({ projectKey }: { projectKey?: string }) {
  const { data: roleData } = useRoles();
  const { data: groupData } = useGroups();
  const grant = useGrantRole();

  const roles = roleData?.roles ?? [];
  const groups = groupData?.groups ?? [];

  const [subject, setSubject] = useState("");
  const [role, setRole] = useState<Role>("user");
  const [scope, setScope] = useState(projectKey ?? "");

  const chosen = roles.find((each) => each.role === role);
  // Giving global administration over one project would read as a smaller thing
  // than it is, so the server refuses it and the form does not offer it.
  const scopeIsFixed = chosen?.orgWideOnly ?? false;

  function submit() {
    if (!subject) return;
    const [kind, id] = subject.split(":");
    grant.mutate({
      role,
      projectKey: scopeIsFixed ? undefined : scope || undefined,
      userId: kind === "user" ? id : undefined,
      groupId: kind === "group" ? id : undefined,
    });
  }

  return (
    <Card className="space-y-3 p-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-end">
        {/* A portal customer holds no seat, so there is nothing to give them. */}
        <PersonPicker
          label={t.access.grants.who}
          aria-label={t.access.grants.whoLabel}
          none={t.access.grants.choose}
          staff
          group={t.access.grants.people}
          optionValue={(person) => `user:${person.id}`}
          value={subject}
          onChange={setSubject}
        >
          {groups.length > 0 && (
            <optgroup label={t.access.grants.groups}>
              {groups.map((group) => (
                <option key={group.id} value={`group:${group.id}`}>
                  {group.name}
                </option>
              ))}
            </optgroup>
          )}
        </PersonPicker>

        <Select label={t.access.grants.role} aria-label={t.access.grants.role} value={role} onChange={(event) => setRole(event.target.value as Role)}>
          {roles.map((each) => (
            <option key={each.role} value={each.role}>
              {each.name}
            </option>
          ))}
        </Select>

        <div className={cx(scopeIsFixed && "opacity-55")}>
          <ProjectPicker
            label={t.access.grants.where}
            aria-label={t.access.grants.whereLabel}
            none={t.access.grants.everyProject}
            value={scopeIsFixed ? "" : scope}
            disabled={scopeIsFixed || Boolean(projectKey)}
            onChange={setScope}
          />
        </div>

        <Button disabled={!subject} loading={grant.isPending} onClick={submit}>
          {t.access.grants.grant}
        </Button>
      </div>

      {chosen && <p className="text-xs text-ink-subtle">{t.access.grants.can(chosen.name, chosen.permissions.map(permissionWords).join(", "))}</p>}
      {grant.error && <ErrorBanner>{(grant.error as Error).message}</ErrorBanner>}
    </Card>
  );
}
