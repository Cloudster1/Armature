import { useQueryClient } from "@tanstack/react-query";
import { useUpdateProject, type PlanningMethod, type Project, type ResourceGrouping } from "@/api/projects";
import { resourcesQueryKey } from "@/api/resources";
import { Card, ErrorBanner, OptionCard, SectionTitle, useToast } from "@/components/ui";
import { t } from "@/i18n";

/** Why a scrum project offers no people; the server refuses with the same sentence in English. */
export function scrumByTeam(): string {
  return t.projectSetup.planning.scrumByTeam;
}

// Built while drawing, so the words follow the language.
function methods(): Array<{ value: PlanningMethod; title: string; description: string; saved: string }> {
  const words = t.projectSetup.planning;
  return [
    { value: "scrum", ...words.scrum },
    { value: "kanban", ...words.kanban },
  ];
}

function groupings(): Array<{ value: ResourceGrouping; title: string; description: string; saved: string }> {
  const words = t.projectSetup.planning;
  return [
    { value: "team", ...words.byTeam },
    { value: "person", ...words.byPerson },
  ];
}

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
      <SectionTitle className="mb-2">{t.projectSetup.planning.title}</SectionTitle>
      <Card className="space-y-4 p-5">
        <p className="text-sm text-ink-muted">{t.projectSetup.planning.detail}</p>
        <div role="radiogroup" aria-label={t.projectSetup.planning.method} className="grid gap-2 sm:grid-cols-2">
          {methods().map((m) => (
            <OptionCard
              key={m.value}
              title={m.title}
              description={m.description}
              checked={project.planningMethod === m.value}
              disabled={update.isPending}
              data-planning-method={m.value}
              onSelect={() => m.value !== project.planningMethod && save({ planningMethod: m.value }, m.saved)}
            />
          ))}
        </div>
        <div role="radiogroup" aria-label={t.projectSetup.planning.grouping} className="grid gap-2 sm:grid-cols-2">
          {groupings().map((g) => {
            const refused = g.value === "person" && project.planningMethod === "scrum";
            return (
              <OptionCard
                key={g.value}
                title={g.title}
                description={refused ? scrumByTeam() : g.description}
                checked={project.resourceGrouping === g.value}
                disabled={refused || update.isPending}
                aria-disabled={refused}
                className={refused ? "cursor-not-allowed opacity-60" : undefined}
                data-resource-grouping={g.value}
                onSelect={() => g.value !== project.resourceGrouping && save({ resourceGrouping: g.value }, g.saved)}
              />
            );
          })}
        </div>
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
      </Card>
    </section>
  );
}
