import { useState } from "react";
import { useAllocations, useSetAllocation, type Allocation } from "@/api/resources";
import { Button, Card, ErrorBanner, Input, SectionTitle, useToast } from "@/components/ui";
import { SHARE_FULL_PERCENT } from "@/config";
import { t } from "@/i18n";

// How much of each person's week the project has. A share is a statement the
// Resources page counts hours at; going past a whole week warns and is kept.
export function ShareSettings({ projectKey }: { projectKey: string }) {
  const { data, error } = useAllocations(projectKey);
  const people = data?.allocations ?? [];

  return (
    <section className="mt-8" data-share-settings>
      <SectionTitle className="mb-2">{t.projectSetup.share.title}</SectionTitle>
      <Card className="space-y-3 p-5">
        <p className="text-sm text-ink-muted">{t.projectSetup.share.detail}</p>
        {error && <ErrorBanner>{(error as Error).message}</ErrorBanner>}
        {data && people.length === 0 && <p className="text-sm text-ink-subtle">{t.projectSetup.share.nobody}</p>}
        {people.length > 0 && (
          <ul className="divide-y divide-border">
            {people.map((person) => (
              <ShareRow key={person.userId} projectKey={projectKey} person={person} />
            ))}
          </ul>
        )}
      </Card>
    </section>
  );
}

function ShareRow({ projectKey, person }: { projectKey: string; person: Allocation }) {
  const [draft, setDraft] = useState(String(person.percent));
  const set = useSetAllocation(projectKey);
  const toast = useToast();
  const percent = Number(draft);
  const valid = draft.trim() !== "" && Number.isInteger(percent) && percent >= 0 && percent <= SHARE_FULL_PERCENT;
  const total = (valid ? percent : person.percent) + person.elsewherePercent;

  function save() {
    set.mutate({ userId: person.userId, percent }, { onSuccess: () => toast.success(t.projectSetup.share.saved(person.name, percent)) });
  }

  return (
    <li className="flex flex-wrap items-center gap-3 py-2" data-share-person={person.name}>
      <span className="min-w-40 flex-1 text-sm text-ink">{person.name}</span>
      <span className="flex items-center gap-1">
        <Input
          type="number"
          min={0}
          max={SHARE_FULL_PERCENT}
          step={1}
          controlSize="sm"
          className="w-20 text-right"
          aria-label={t.projectSetup.share.input(person.name)}
          invalid={!valid}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          data-share-input={person.name}
        />
        <span className="text-sm text-ink-muted">%</span>
      </span>
      <Button
        size="sm"
        variant="secondary"
        aria-label={t.projectSetup.share.saveLabel(person.name)}
        disabled={!valid || percent === person.percent}
        loading={set.isPending}
        onClick={save}
        data-action="save-share"
      >
        {t.projectSetup.share.save}
      </Button>
      {total > SHARE_FULL_PERCENT && (
        <p className="w-full text-xs text-warning" data-share-warning={person.name}>
          {t.projectSetup.share.over(person.name, total)}
        </p>
      )}
      {set.error && (
        <div className="w-full">
          <ErrorBanner>{(set.error as Error).message}</ErrorBanner>
        </div>
      )}
    </li>
  );
}
