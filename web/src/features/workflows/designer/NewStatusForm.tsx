import { useState, type FormEvent } from "react";
import { useCreateStatus, type Status, type StatusCategory } from "@/api/issues";
import { Button, ErrorBanner, Field, Select } from "@/components/ui";
import { t } from "@/i18n";

// The words are read when the form draws, so they follow the language.
const categories: StatusCategory[] = ["todo", "in_progress", "done"];

/**
 * Coins a status the organization does not have yet, right where it is
 * wanted. The category is chosen here and is for keeps: everything that
 * counts what is done reads it.
 */
export function NewStatusForm({
  onCreated,
  heading = t.workflows.newStatus.heading,
  submitLabel = t.workflows.newStatus.submit,
}: {
  onCreated: (status: Status) => void;
  heading?: string;
  submitLabel?: string;
}) {
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
      <Field
        id="field-new-status-name"
        label={t.workflows.newStatus.name}
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder={t.workflows.newStatus.namePlaceholder}
      />
      <Select
        id="field-new-status-category"
        label={t.workflows.newStatus.countsAs}
        value={category}
        onChange={(e) => setCategory(e.target.value as StatusCategory)}
      >
        {categories.map((c) => (
          <option key={c} value={c}>
            {t.workflows.categories[c]}
          </option>
        ))}
      </Select>
      <Field
        id="field-new-status-description"
        label={t.workflows.newStatus.description}
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        placeholder={t.workflows.newStatus.descriptionPlaceholder}
      />
      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
      <Button type="submit" size="sm" variant="secondary" loading={create.isPending} disabled={!name.trim()}>
        {submitLabel}
      </Button>
    </form>
  );
}
