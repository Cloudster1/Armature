import type { PlanningMethod, ResourceGrouping } from "@/api/projects";
import { Button, Segmented, Tag, Toolbar } from "@/components/ui";
import { t } from "@/i18n";

// Read while drawing, so the words follow the language.
const groupings = (): Array<{ value: ResourceGrouping; label: string }> => [
  { value: "team", label: t.schedule.resources.teams },
  { value: "person", label: t.schedule.resources.people },
];

/** What a project that does not offer the switch says instead. */
export function plannedBy(method: PlanningMethod, grouping: ResourceGrouping): string {
  if (method === "scrum") return t.schedule.resources.plannedByTeam;
  return grouping === "person" ? t.schedule.resources.plannedByPerson : t.schedule.resources.plannedByTeam;
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
      label={t.schedule.resources.controls}
      start={
        switchable ? (
          <Segmented<ResourceGrouping>
            label={t.schedule.resources.planBy}
            value={grouping}
            onChange={(next) => !saving && next !== grouping && onGrouping(next)}
            options={groupings().map((g) => ({ ...g, attrs: { "data-action": `group-by-${g.value}` } }))}
          />
        ) : (
          <Tag
            data-resource-grouping={method === "scrum" ? "team" : grouping}
            title={method === "scrum" ? t.schedule.resources.scrumPlansByTeam : t.schedule.resources.adminsChoose}
          >
            {plannedBy(method, grouping)}
          </Tag>
        )
      }
      end={
        <>
          <Button size="sm" variant="ghost" onClick={onPrevious} data-action="resources-prev">
            {t.schedule.resources.previous}
          </Button>
          <Button size="sm" variant="ghost" onClick={onToday} data-action="resources-today">
            {t.schedule.resources.today}
          </Button>
          <Button size="sm" variant="ghost" onClick={onNext} data-action="resources-next">
            {t.schedule.resources.next}
          </Button>
        </>
      }
    />
  );
}
