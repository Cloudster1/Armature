import { useState, type FormEvent } from "react";
import { useSaveTemplate, type Dashboard } from "@/api/reports";
import { Button, Dialog, ErrorBanner, Field, useToast } from "@/components/ui";

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
          toast.success(`Saved ${made.template.name} as a template`);
          onClose();
        },
      },
    );
  }
  return (
    <Dialog open={open} onClose={onClose} title="Save as a template" description="The arrangement and each widget's settings are kept for the whole organization. Anything tied to this project, such as a team, is left out." attrs={{ "data-save-template": "" }}>
      <form onSubmit={onSubmit} className="space-y-3" noValidate>
        <Field label="Template name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        <Field label="Description" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What it is for" />
        {save.error && <ErrorBanner>{(save.error as Error).message}</ErrorBanner>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={save.isPending} disabled={!name.trim()}>
            Save template
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
