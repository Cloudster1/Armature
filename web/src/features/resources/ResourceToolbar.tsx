import type { PlanningMethod, ResourceGrouping } from "@/api/projects";
import { Button, Segmented, Tag, Toolbar } from "@/components/ui";

const GROUPINGS: Array<{ value: ResourceGrouping; label: string }> = [
  { value: "team", label: "Teams" },
  { value: "person", label: "People" },
];

/** What a project that does not offer the switch says instead. */
export function plannedBy(method: PlanningMethod, grouping: ResourceGrouping): string {
  if (method === "scrum") return "Planned by team";
  return grouping === "person" ? "Planned by person" : "Planned by team";
}

// The window and, for kanban, the grouping. Only somebody who administers the
// project switches it, since it is the project's choice and not a view.
export function ResourceToolbar({
  method,
  grouping,
  canChange,
  saving,
  onGrouping,
  onPrevious,
  onToday,
  onNext,
}: {
  method: PlanningMethod;
  grouping: ResourceGrouping;
  canChange: boolean;
  saving: boolean;
  onGrouping: (grouping: ResourceGrouping) => void;
  onPrevious: () => void;
  onToday: () => void;
  onNext: () => void;
}) {
  const switchable = method === "kanban" && canChange;
  return (
    <Toolbar
      label="Resource controls"
      start={
        switchable ? (
          <Segmented<ResourceGrouping>
            label="Plan by"
            value={grouping}
            onChange={(next) => !saving && next !== grouping && onGrouping(next)}
            options={GROUPINGS.map((g) => ({ ...g, attrs: { "data-action": `group-by-${g.value}` } }))}
          />
        ) : (
          <Tag data-resource-grouping={method === "scrum" ? "team" : grouping} title={method === "scrum" ? "A scrum project plans by team." : "The project's administrators choose this in its settings."}>
            {plannedBy(method, grouping)}
          </Tag>
        )
      }
      end={
        <>
          <Button size="sm" variant="ghost" onClick={onPrevious} data-action="resources-prev">
            Previous
          </Button>
          <Button size="sm" variant="ghost" onClick={onToday} data-action="resources-today">
            Today
          </Button>
          <Button size="sm" variant="ghost" onClick={onNext} data-action="resources-next">
            Next
          </Button>
        </>
      }
    />
  );
}
