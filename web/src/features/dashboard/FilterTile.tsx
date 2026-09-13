import { useEffect, useState, type FormEvent } from "react";
import { useIssueTypes, useMembers } from "@/api/issues";
import { useMilestones } from "@/api/milestones";
import { useTeams } from "@/api/teams";
import { Button, Chip, Field, Segmented, Select, Tag } from "@/components/ui";
import { DASHBOARD_FILTER_WINDOWS } from "@/config";
import { TypeBadge } from "@/features/issues/badges";
import { caretLine } from "@/features/search/help";
import { UNASSIGNED, activeCount, composeQuery, type Category, type DateField, type FilterSpec, type SavedQueryLookup } from "./filter";
import { useSavedFilters } from "@/api/filters";

/** What the server said about the composed query, when it refused it. */
export interface QueryFailure {
  message: string;
  position?: number;
}

/**
 * The tile that narrows the dashboard. Its controls write the search
 * language, so what it says here is what every counting widget is asked.
 */
export function FilterTile({
  projectKey,
  spec,
  onChange,
  failure,
  arranging,
  saved,
  onSaveDefault,
  saving,
  savedQuery = () => undefined,
}: {
  projectKey: string;
  spec: FilterSpec;
  onChange: (next: FilterSpec) => void;
  failure?: QueryFailure;
  arranging: boolean;
  /** The defaults the tile stores; the button to save is offered when the live filter differs. */
  saved: FilterSpec;
  onSaveDefault: (spec: FilterSpec) => void;
  saving: boolean;
  savedQuery?: SavedQueryLookup;
}) {
  const { data: savedData } = useSavedFilters();
  const { data: typeData } = useIssueTypes();
  const { data: teamData } = useTeams(projectKey);
  const { data: memberData } = useMembers();
  const { data: milestoneData } = useMilestones(projectKey, true);
  const types = (typeData?.issueTypes ?? []).filter((type) => !type.isSubtask);
  const teams = teamData?.teams ?? [];
  const members = memberData?.members ?? [];
  const milestones = milestoneData?.milestones ?? [];
  const active = activeCount(spec);
  const dirty = JSON.stringify(spec) !== JSON.stringify(saved);

  // The query field applies on Enter or when focus leaves, not on every key.
  const [draft, setDraft] = useState(spec.query);
  useEffect(() => setDraft(spec.query), [spec.query]);
  function applyQuery(event?: FormEvent) {
    event?.preventDefault();
    if (draft.trim() !== spec.query.trim()) onChange({ ...spec, query: draft.trim() });
  }

  function toggleType(name: string) {
    const types = spec.types.includes(name) ? spec.types.filter((each) => each !== name) : [...spec.types, name];
    onChange({ ...spec, types });
  }

  return (
    <div className="space-y-4" data-filter-tile="">
      <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Filter by type">
        {types.map((type) => {
          const on = spec.types.includes(type.name);
          return (
            <Chip key={type.id} pressed={on} onClick={() => toggleType(type.name)} data-filter-type={type.name}>
              <TypeBadge icon={type.icon} name={type.name} />
              {type.name}
            </Chip>
          );
        })}
        <span className="flex-1" />
        {active > 0 && (
          <Tag className="bg-accent-subtle text-accent" data-filter-count={active}>
            {active} {active === 1 ? "filter" : "filters"}
          </Tag>
        )}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <Select label="Team" value={spec.team} onChange={(event) => onChange({ ...spec, team: event.target.value })}>
          <option value="">Any team</option>
          {teams.map((team) => (
            <option key={team.id} value={team.name}>
              {team.name}
            </option>
          ))}
        </Select>
        <Select label="Assignee" value={spec.assignee} onChange={(event) => onChange({ ...spec, assignee: event.target.value })}>
          <option value="">Anyone</option>
          <option value={UNASSIGNED}>Nobody yet</option>
          {members.map((member) => (
            <option key={member.id} value={member.email ?? member.name}>
              {member.name}
            </option>
          ))}
        </Select>
        <Select label="Saved search" id="field-saved-search" value={spec.filterId} onChange={(event) => onChange({ ...spec, filterId: event.target.value })} hint={spec.filterId ? savedQuery(spec.filterId) : undefined}>
          <option value="">None</option>
          {(savedData?.filters ?? []).map((f) => (
            <option key={f.id} value={f.id}>
              {f.name}
            </option>
          ))}
        </Select>
        <Select label="Milestone" value={spec.milestone} onChange={(event) => onChange({ ...spec, milestone: event.target.value })}>
          <option value="">Any milestone</option>
          {milestones.map((milestone) => (
            <option key={milestone.id} value={milestone.name}>
              {milestone.name}
              {milestone.closedAt ? " (closed)" : ""}
            </option>
          ))}
        </Select>
        <Select label="Since" value={String(spec.days)} onChange={(event) => onChange({ ...spec, days: Number(event.target.value) })}>
          <option value="0">Any time</option>
          {DASHBOARD_FILTER_WINDOWS.map((days) => (
            <option key={days} value={days}>
              Last {days} days
            </option>
          ))}
        </Select>
        <Select label="Counted by" value={spec.field} disabled={spec.days === 0} onChange={(event) => onChange({ ...spec, field: event.target.value as DateField })}>
          <option value="created">When created</option>
          <option value="updated">When updated</option>
          <option value="resolved">When resolved</option>
        </Select>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <Segmented<Category>
          label="Status"
          value={spec.category}
          onChange={(category) => onChange({ ...spec, category })}
          options={[
            { value: "", label: "Any status", attrs: { "data-filter-category": "any" } },
            { value: "todo", label: "To do", attrs: { "data-filter-category": "todo" } },
            { value: "in_progress", label: "In progress", attrs: { "data-filter-category": "in_progress" } },
            { value: "done", label: "Done", attrs: { "data-filter-category": "done" } },
          ]}
        />
        <form onSubmit={applyQuery} className="min-w-64 flex-1" noValidate>
          <Field
            label="Narrow with a query"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onBlur={() => applyQuery()}
            placeholder="labels = urgent OR priority = highest"
            spellCheck={false}
            autoComplete="off"
            className="font-mono text-sm"
            error={failure?.message}
            hint="Anything the controls cannot say, in the language of Search."
          />
          {failure && failure.position !== undefined && (
            <pre className="m-0 mt-1 overflow-x-auto font-mono text-sm leading-tight text-ink" data-query-error>
              {composeQuery(spec, savedQuery)}
              {"\n"}
              <span className="text-danger" data-query-caret>
                {caretLine(failure.position)}
              </span>
            </pre>
          )}
        </form>
        {active > 0 && (
          <Button variant="ghost" onClick={() => onChange({ ...spec, types: [], team: "", assignee: "", category: "", days: 0, milestone: "", query: "", filterId: "" })}>
            Clear filter
          </Button>
        )}
        {arranging && (
          <Button variant="secondary" disabled={!dirty} loading={saving} onClick={() => onSaveDefault(spec)} data-action="save-filter">
            Save as the default
          </Button>
        )}
      </div>
    </div>
  );
}
