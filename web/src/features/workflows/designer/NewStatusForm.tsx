import { useState, type FormEvent } from "react";
import { useCreateStatus, type Status, type StatusCategory } from "@/api/issues";
import { Button, ErrorBanner, Field, Select } from "@/components/ui";

const categories: Array<{ value: StatusCategory; label: string }> = [
  { value: "todo", label: "To do" },
  { value: "in_progress", label: "In progress" },
  { value: "done", label: "Done" },
];

/**
 * Coins a status the organization does not have yet, right where it is
 * wanted. The category is chosen here and is for keeps: everything that
 * counts what is done reads it.
 */
export function NewStatusForm({ onCreated, heading = "Or coin a new one", submitLabel = "Create and add" }: { onCreated: (status: Status) => void; heading?: string; submitLabel?: string }) {
  const create = useCreateStatus();
  const [name, setName] = useState("");
  const [category, setCategory] = useState<StatusCategory>("in_progress");
  const [description, setDescription] = useState("");

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    create.mutate(
      { name: name.trim(), category, description: description.trim() || undefined },
      {
        onSuccess: ({ status }) => {
          setName("");
          setDescription("");
          onCreated(status);
        },
      },
    );
  }

  return (
    <form onSubmit={onSubmit} className="space-y-2" noValidate data-testid="new-status">
      <span className="block text-sm font-medium text-ink-muted">{heading}</span>
      <Field id="field-new-status-name" label="Status name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Reviewing" />
      <Select id="field-new-status-category" label="Counts as" value={category} onChange={(e) => setCategory(e.target.value as StatusCategory)}>
        {categories.map((c) => (
          <option key={c.value} value={c.value}>
            {c.label}
          </option>
        ))}
      </Select>
      <Field
        id="field-new-status-description"
        label="Description"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        placeholder="What being here means. Optional."
      />
      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
      <Button type="submit" size="sm" variant="secondary" loading={create.isPending} disabled={!name.trim()}>
        {submitLabel}
      </Button>
    </form>
  );
}
