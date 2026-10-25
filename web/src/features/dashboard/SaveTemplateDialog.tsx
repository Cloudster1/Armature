import { useState, type FormEvent } from "react";
import { useSaveTemplate, type Dashboard } from "@/api/reports";
import { Button, Dialog, ErrorBanner, Field, useToast } from "@/components/ui";
import { t } from "@/i18n";

/** Keeps the dashboard's arrangement as one of the organization's templates. */
export function SaveTemplateDialog({ dashboard, open, onClose }: { dashboard: Dashboard; open: boolean; onClose: () => void }) {
  const save = useSaveTemplate();
  const toast = useToast();
  const [name, setName] = useState(dashboard.name);
  const [description, setDescription] = useState("");
  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    save.mutate(
      { dashboardId: dashboard.id, name: name.trim(), description: description.trim() },
      {
        onSuccess: (made) => {
          toast.success(t.dashboard.templates.saved(made.template.name));
          onClose();
        },
      },
    );
  }
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t.dashboard.templates.saveTitle}
      description={t.dashboard.templates.saveDescription}
      attrs={{ "data-save-template": "" }}
    >
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label={t.dashboard.templates.name} autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        <Field
          label={t.dashboard.templates.description}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder={t.dashboard.templates.descriptionPlaceholder}
        />
        {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            {t.kit.cancel}
          </Button>
          <Button type="submit" loading={save.isPending} disabled={!name.trim()}>
            {t.dashboard.templates.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
