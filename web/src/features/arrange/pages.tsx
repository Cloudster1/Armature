import { useArrangements, useOrgArrangements, useSetOrgArrangement, useSetProjectArrangement } from "@/api/arrange";
import { useOrgFields, useProjectFields } from "@/api/fields";
import { Skeleton } from "@/components/ui";
import { ArrangementEditor } from "./ArrangementEditor";

/** How this project draws an issue of each type, and how to change it. */
export function ProjectArrangement({ projectKey }: { projectKey: string }) {
  const { data, isLoading } = useArrangements(projectKey);
  const { data: fields } = useProjectFields(projectKey);
  const save = useSetProjectArrangement(projectKey);
  if (isLoading) return <Skeleton lines={6} />;
  return (
    <ArrangementEditor
      arrangements={data?.arrangements ?? []}
      fields={fields?.fields ?? []}
      saving={save.isPending}
      error={save.error as Error | null}
      canFollow
      onSave={save.mutate}
    />
  );
}

/** What every project follows until it arranges an issue differently. */
export function OrgArrangement({ canConfigure }: { canConfigure: boolean }) {
  const { data, isLoading } = useOrgArrangements(canConfigure);
  const { data: fields } = useOrgFields();
  const save = useSetOrgArrangement();
  if (isLoading) return <Skeleton lines={6} />;
  return (
    <ArrangementEditor
      arrangements={data?.arrangements ?? []}
      fields={fields?.fields ?? []}
      saving={save.isPending}
      error={save.error as Error | null}
      canFollow={false}
      onSave={save.mutate}
    />
  );
}
