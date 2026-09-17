import { useState } from "react";
import { useMembers } from "@/api/issues";
import { useProjects } from "@/api/projects";
import {
  permissionWords,
  roleName,
  useAssignments,
  useGrantRole,
  useGroups,
  useRevokeRole,
  useRoles,
  type Assignment,
  type Role,
} from "@/api/access";
import { Button, Card, cx, EmptyState, ErrorBanner, Select, Table, Td, Th } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/**
 * Who holds what.
 *
 * A grant is a role, a scope and a subject: a person or a group, over the whole
 * organization or over one project. Reading the table should answer "why can
 * this person do that" without anybody having to know the schema.
 */
export function RoleTable({ projectKey }: { projectKey?: string }) {
  const { data, isLoading, error } = useAssignments(projectKey);
  const revoke = useRevokeRole();
  const confirm = useConfirm();

  const assignments = data?.assignments ?? [];
  const failure = (error ?? revoke.error) as Error | undefined;

  return (
    <div className="space-y-4">
      <GrantForm projectKey={projectKey} />

      {failure && <ErrorBanner>{failure.message}</ErrorBanner>}
      {isLoading && <p className="text-sm text-ink-muted">Loading...</p>}

      {!isLoading && assignments.length === 0 ? (
        <EmptyState
          title="Nobody has been given anything yet"
          description="Roles can be given to a person or to a group, over this project or over the whole organization."
        />
      ) : (
        <Table data-testid="role-assignments">
          <thead>
            <tr>
              <Th>Who</Th>
              <Th>Role</Th>
              <Th>Where</Th>
              <Th>
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {assignments.map((assignment) => (
              <AssignmentRow
                key={assignment.id}
                assignment={assignment}
                onRevoke={async () => (await confirm({ noun: "role", verb: "Revoke", body: `${roleName(assignment.role)} is taken away at once; what it allowed is refused from the next request.` })) && revoke.mutate(assignment.id)}
              />
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}

function AssignmentRow({ assignment, onRevoke }: { assignment: Assignment; onRevoke: () => void }) {
  const toGroup = Boolean(assignment.groupId);
  return (
    <tr data-assignment={assignment.id}>
      <Td className="w-full min-w-48">
        <span className="text-ink">{assignment.groupName || assignment.userName}</span>
        {toGroup && <span className="ml-2 rounded-full bg-surface-raised px-2 py-0.5 text-2xs text-ink-muted">group</span>}
      </Td>
      <Td className="whitespace-nowrap text-ink">{roleName(assignment.role)}</Td>
      <Td className="whitespace-nowrap text-ink-muted">{assignment.projectKey || "Every project"}</Td>
      <Td className="text-right">
        <Button size="sm" variant="ghost" onClick={onRevoke}>
          Revoke
        </Button>
      </Td>
    </tr>
  );
}

function GrantForm({ projectKey }: { projectKey?: string }) {
  const { data: roleData } = useRoles();
  const { data: memberData } = useMembers();
  const { data: groupData } = useGroups();
  const { data: projectData } = useProjects();
  const grant = useGrantRole();

  const roles = roleData?.roles ?? [];
  // A portal customer holds no seat, so there is nothing to give them.
  const people = (memberData?.members ?? []).filter((person) => person.role !== "customer");
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
        <Select label="Who" aria-label="Who to grant to" value={subject} onChange={(event) => setSubject(event.target.value)}>
          <option value="">Choose a person or a group</option>
          {groups.length > 0 && (
            <optgroup label="Groups">
              {groups.map((group) => (
                <option key={group.id} value={`group:${group.id}`}>
                  {group.name}
                </option>
              ))}
            </optgroup>
          )}
          <optgroup label="People">
            {people.map((person) => (
              <option key={person.id} value={`user:${person.id}`}>
                {person.name}
              </option>
            ))}
          </optgroup>
        </Select>

        <Select label="Role" aria-label="Role" value={role} onChange={(event) => setRole(event.target.value as Role)}>
          {roles.map((each) => (
            <option key={each.role} value={each.role}>
              {roleName(each.role)}
            </option>
          ))}
        </Select>

        <div className={cx(scopeIsFixed && "opacity-55")}>
          <Select
            label="Where"
            aria-label="Where the role applies"
            value={scopeIsFixed ? "" : scope}
            disabled={scopeIsFixed || Boolean(projectKey)}
            onChange={(event) => setScope(event.target.value)}
          >
            <option value="">Every project</option>
            {(projectData?.projects ?? []).map((project) => (
              <option key={project.key} value={project.key}>
                {project.key}
              </option>
            ))}
          </Select>
        </div>

        <Button disabled={!subject} loading={grant.isPending} onClick={submit}>
          Grant
        </Button>
      </div>

      {chosen && (
        <p className="text-xs text-ink-subtle">
          {roleName(chosen.role)} can {chosen.permissions.map(permissionWords).join(", ")}.
        </p>
      )}
      {grant.error && <ErrorBanner>{(grant.error as Error).message}</ErrorBanner>}
    </Card>
  );
}
