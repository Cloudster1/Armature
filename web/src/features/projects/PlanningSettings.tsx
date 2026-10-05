import { useQueryClient } from "@tanstack/react-query";
import { useUpdateProject, type PlanningMethod, type Project, type ResourceGrouping } from "@/api/projects";
import { resourcesQueryKey } from "@/api/resources";
import { Card, ErrorBanner, OptionCard, SectionTitle, useToast } from "@/components/ui";

/** Why a scrum project offers no people; the server refuses with the same sentence. */
export const SCRUM_BY_TEAM = "A scrum project plans by team. Switch the project to kanban to plan by person.";

const METHODS: Array<{ value: PlanningMethod; title: string; description: string }> = [
  { value: "scrum", title: "Scrum", description: "Work is committed in sprints, and a sprint is a team's." },
  { value: "kanban", title: "Kanban", description: "Work flows; it is planned against teams or against people." },
];

const GROUPINGS: Array<{ value: ResourceGrouping; title: string; description: string }> = [
  { value: "team", title: "By team", description: "Each team's members' hours together." },
  { value: "person", title: "By person", description: "Each person's own hours, and the work assigned to them." },
];

// How the project plans, saved as soon as it is chosen like the features are.
// Turning to scrum takes the grouping back to teams; the server says so in its answer.
export function PlanningSettings({ project }: { project: Project }) {
  const update = useUpdateProject();
  const queryClient = useQueryClient();
  const toast = useToast();

  function save(change: { planningMethod?: PlanningMethod; resourceGrouping?: ResourceGrouping }, said: string) {
    update.mutate(
      { key: project.key, ...change },
      {
        onSuccess: () => {
          void queryClient.invalidateQueries({ queryKey: resourcesQueryKey });
          toast.success(said);
        },
      },
    );
  }

  return (
    <section className="mt-8" data-planning-settings>
      <SectionTitle className="mb-2">Planning</SectionTitle>
      <Card className="space-y-4 p-5">
        <p className="text-sm text-ink-muted">What the Resources page sets the scheduled work against, week by week, in hours.</p>
        <div role="radiogroup" aria-label="Planning method" className="grid gap-2 sm:grid-cols-2">
          {METHODS.map((m) => (
            <OptionCard
              key={m.value}
              title={m.title}
              description={m.description}
              checked={project.planningMethod === m.value}
              disabled={update.isPending}
              data-planning-method={m.value}
              onSelect={() => m.value !== project.planningMethod && save({ planningMethod: m.value }, `Planning as ${m.title.toLowerCase()}`)}
            />
          ))}
        </div>
        <div role="radiogroup" aria-label="Plan resources" className="grid gap-2 sm:grid-cols-2">
          {GROUPINGS.map((g) => {
            const refused = g.value === "person" && project.planningMethod === "scrum";
            return (
              <OptionCard
                key={g.value}
                title={g.title}
                description={refused ? SCRUM_BY_TEAM : g.description}
                checked={project.resourceGrouping === g.value}
                disabled={refused || update.isPending}
                aria-disabled={refused}
                className={refused ? "cursor-not-allowed opacity-60" : undefined}
                data-resource-grouping={g.value}
                onSelect={() => g.value !== project.resourceGrouping && save({ resourceGrouping: g.value }, `Planning ${g.title.toLowerCase()}`)}
              />
            );
          })}
        </div>
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      </Card>
    </section>
  );
}
