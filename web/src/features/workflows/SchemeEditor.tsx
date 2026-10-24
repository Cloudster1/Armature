import { useState } from "react";
import { useIssueTypes } from "@/api/issues";
import { useCreateScheme, useSaveScheme, type Scheme, type SchemeInput } from "@/api/workflows";
import { Button, Card, ErrorBanner, Field, SelectInput } from "@/components/ui";
import { WorkflowPicker } from "./WorkflowPicker";

/** A mapping as the editor holds it. An empty issue type is the fallback; an empty workflow is not chosen yet. */
interface DraftItem {
  issueTypeId: string;
  workflowId: string;
  workflowName: string;
}

const FALLBACK = "";

/** A new mapping names no workflow until somebody picks one. */
const UNCHOSEN: DraftItem = { issueTypeId: FALLBACK, workflowId: "", workflowName: "" };

/**
 * Edits the mappings that make up a scheme.
 *
 * The organization's scheme needs a fallback, because an issue type nobody
 * mapped would otherwise have no workflow at all. A project's scheme does not:
 * it is allowed to be a short list of disagreements, and everything it leaves
 * out falls through to the organization.
 */
export function SchemeEditor({ scheme, onDone }: { scheme?: Scheme; onDone: () => void }) {
  const { data: typeData } = useIssueTypes();
  const issueTypes = typeData?.issueTypes ?? [];

  const [name, setName] = useState(scheme?.name ?? "");
  const [items, setItems] = useState<DraftItem[]>(() =>
    scheme
      ? scheme.items.map((item) => ({
          issueTypeId: item.issueTypeId ?? FALLBACK,
          workflowId: item.workflowId,
          workflowName: item.workflowName,
        }))
      : [{ ...UNCHOSEN }],
  );

  const create = useCreateScheme();
  const save = useSaveScheme();
  const pending = create.isPending || save.isPending;
  const error = (create.error ?? save.error) as Error | undefined;

  function submit() {
    const input: SchemeInput = {
      name,
      items: items.map((item) => ({
        issueTypeId: item.issueTypeId === FALLBACK ? null : item.issueTypeId,
        workflowId: item.workflowId,
      })),
    };
    const done = { onSuccess: onDone };
    if (scheme) {
      save.mutate({ id: scheme.id, ...input }, done);
      return;
    }
    create.mutate(input, done);
  }

  return (
    <Card className="space-y-4 p-4">
      <Field label="Scheme name" value={name} placeholder="Support project workflows" onChange={(event) => setName(event.target.value)} />

      <div className="space-y-2">
        {items.map((item, at) => (
          <div key={at} className="flex flex-wrap items-center gap-2">
            <SelectInput
              aria-label="Issue type"
              value={item.issueTypeId}
              onChange={(event) => setItems((current) => current.map((each, index) => (index === at ? { ...each, issueTypeId: event.target.value } : each)))}
              className="w-44"
            >
              <option value={FALLBACK}>Everything else</option>
              {issueTypes.map((type) => (
                <option key={type.id} value={type.id}>
                  {type.name}
                </option>
              ))}
            </SelectInput>
            <span aria-hidden="true" className="text-ink-subtle">
              uses
            </span>
            <WorkflowPicker
              aria-label="Workflow"
              value={item.workflowId}
              chosenName={item.workflowName}
              leading={!item.workflowId && <option value="">Choose a workflow</option>}
              onChange={(workflowId, workflowName) =>
                setItems((current) => current.map((each, index) => (index === at ? { ...each, workflowId, workflowName } : each)))
              }
            />
            <Button variant="ghost" size="sm" onClick={() => setItems((current) => current.filter((_, index) => index !== at))}>
              Remove
            </Button>
          </div>
        ))}
      </div>

      <Button variant="secondary" size="sm" onClick={() => setItems((current) => [...current, { ...UNCHOSEN }])}>
        Add a mapping
      </Button>

      {error && <ErrorBanner>{error.message}</ErrorBanner>}

      <div className="flex gap-2">
        <Button loading={pending} disabled={items.some((item) => !item.workflowId)} onClick={submit}>
          {scheme ? "Save scheme" : "Create scheme"}
        </Button>
        <Button variant="ghost" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </Card>
  );
}
