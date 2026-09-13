import { useState, type FormEvent } from "react";
import { useDeleteVersion, useCreateVersion, useReleaseNotes, useUpdateVersion, useVersionAction, useVersions, type Version } from "@/api/versions";
import { dayOf, describeProgress } from "@/api/milestones";
import { Button, Card, Drawer, EmptyState, ErrorBanner, Field, IconButton, Menu, Segmented, Table, Tag, Td, Th, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";
import { useConfirm } from "@/features/shell/ConfirmProvider";
import { useFormat } from "@/lib/format";

type View = "unreleased" | "released" | "archived";

/** Which versions a view shows. */
export function inView(v: Pick<Version, "releasedAt" | "archivedAt">, view: View): boolean {
  if (view === "archived") return Boolean(v.archivedAt);
  if (v.archivedAt) return false;
  return view === "released" ? Boolean(v.releasedAt) : !v.releasedAt;
}

/** The notes as text somebody pastes into a changelog. */
export function notesAsText(groups: Array<{ type: string; issues: Array<{ key: string; summary: string }> }>, name: string): string {
  const lines = [`Release ${name}`, ""];
  for (const g of groups) {
    lines.push(`${g.type}`);
    for (const i of g.issues) lines.push(`- ${i.key} ${i.summary}`);
    lines.push("");
  }
  return lines.join("\n").trimEnd() + "\n";
}

// What the project ships: unreleased versions with how far each has got,
// released ones as a record, and a notes panel that lists the finished work.
export function ReleaseList({ projectKey, canPlan }: { projectKey: string; canPlan: boolean }) {
  const { data, isLoading, error } = useVersions(projectKey, true);
  const release = useVersionAction("release");
  const unrelease = useVersionAction("unrelease");
  const archive = useVersionAction("archive");
  const remove = useDeleteVersion();
  const confirm = useConfirm();
  const toast = useToast();
  const format = useFormat();
  const [view, setView] = useState<View>("unreleased");
  const [notesOf, setNotesOf] = useState<Version | null>(null);
  const [editing, setEditing] = useState<Version | null>(null);
  const versions = (data?.versions ?? []).filter((v) => inView(v, view));

  if (error) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;

  return (
    <div className="space-y-4">
      {canPlan && <NewVersion projectKey={projectKey} />}
      <Segmented<View>
        label="Versions"
        value={view}
        onChange={setView}
        options={[
          { value: "unreleased", label: "Unreleased", attrs: { "data-versions-view": "unreleased" } },
          { value: "released", label: "Released", attrs: { "data-versions-view": "released" } },
          { value: "archived", label: "Archived", attrs: { "data-versions-view": "archived" } },
        ]}
      />
      {isLoading ? null : versions.length === 0 ? (
        <EmptyState title={view === "unreleased" ? "Nothing planned" : `Nothing ${view}`} description="A version is what the project ships. Name it on the issues that fix it, and it tracks how close it is; release it, and its notes list the finished work." />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Version</Th>
              <Th>Progress</Th>
              <Th>Release</Th>
              <Th>State</Th>
              <Th className="w-12" />
            </tr>
          </thead>
          <tbody>
            {versions.map((v) => (
              <tr key={v.id} data-version={v.name} data-version-state={v.archivedAt ? "archived" : v.releasedAt ? "released" : "unreleased"}>
                <Td>
                  <span className="block font-medium text-ink">{v.name}</span>
                  {v.description && <span className="block text-sm text-ink-muted">{v.description}</span>}
                </Td>
                <Td className="min-w-48">
                  <span className="block text-sm text-ink-muted" data-version-progress={v.progress.percent}>
                    {describeProgress(v.progress)}
                  </span>
                  <span className="mt-1 block h-1 w-full rounded-full bg-surface-raised" aria-hidden>
                    <span className="block h-1 rounded-full bg-accent" style={{ width: `${v.progress.percent}%` }} />
                  </span>
                </Td>
                <Td className="text-sm text-ink-muted">{v.releasedAt ? `released ${format.relative(v.releasedAt)}` : v.releaseOn ? format.date(v.releaseOn) : "no date"}</Td>
                <Td>
                  <Tag>{v.archivedAt ? "Archived" : v.releasedAt ? "Released" : "Unreleased"}</Tag>
                </Td>
                <Td>
                  <Menu
                    label={`Actions for ${v.name}`}
                    align="end"
                    trigger={(props) => <IconButton icon={<Icon.More />} label={`Actions for ${v.name}`} size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} data-version-menu={v.name} />}
                    items={[
                      { label: "Release notes", icon: <Icon.Download />, onSelect: () => setNotesOf(v), attrs: { "data-action": "version-notes" } },
                      ...(canPlan && !v.archivedAt
                        ? [
                            v.releasedAt
                              ? { label: "Take the release back", icon: <Icon.ChevronLeft />, onSelect: () => unrelease.mutate(v.id), attrs: { "data-action": "version-unrelease" } }
                              : { label: "Release", icon: <Icon.Check />, onSelect: () => release.mutate(v.id, { onSuccess: () => toast.success(`${v.name} released`) }), attrs: { "data-action": "version-release" } },
                            { label: "Edit", icon: <Icon.Edit />, onSelect: () => setEditing(v), attrs: { "data-action": "version-edit" } },
                          ]
                        : []),
                      ...(canPlan && v.releasedAt && !v.archivedAt ? [{ label: "Archive", icon: <Icon.Archive />, onSelect: () => archive.mutate(v.id), attrs: { "data-action": "version-archive" } }] : []),
                      ...(canPlan
                        ? [
                            {
                              label: "Delete",
                              icon: <Icon.Trash />,
                              danger: true,
                              onSelect: async () => (await confirm({ noun: "version", verb: "Delete", body: `${v.name} goes; the issues that named it simply stop naming it.` })) && remove.mutate(v.id),
                              attrs: { "data-action": "version-delete" },
                            },
                          ]
                        : []),
                    ]}
                  />
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {notesOf && <NotesPanel version={notesOf} onClose={() => setNotesOf(null)} />}
      {editing && <EditVersion version={editing} onClose={() => setEditing(null)} />}
    </div>
  );
}

function NewVersion({ projectKey }: { projectKey: string }) {
  const create = useCreateVersion(projectKey);
  const [name, setName] = useState("");
  const [releaseOn, setReleaseOn] = useState("");
  const [description, setDescription] = useState("");

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), description: description.trim(), releaseOn: releaseOn || undefined },
      { onSuccess: () => { setName(""); setReleaseOn(""); setDescription(""); } },
    );
  }

  return (
    <Card className="p-4" data-guide="new-version">
      <form onSubmit={submit} className="flex flex-wrap items-end gap-3" data-testid="new-version">
        <div className="min-w-40 flex-1">
          <Field label="Version" id="field-version-name" value={name} placeholder="1.0" onChange={(e) => setName(e.target.value)} />
        </div>
        <Field label="Release on" id="field-version-release" type="date" value={releaseOn} onChange={(e) => setReleaseOn(e.target.value)} className="w-40" />
        <div className="min-w-56 flex-[2]">
          <Field label="What is in it" id="field-version-description" value={description} placeholder="Optional." onChange={(e) => setDescription(e.target.value)} />
        </div>
        <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
          Add version
        </Button>
      </form>
      {create.error && (
        <div className="mt-3">
          <ErrorBanner>{(create.error as Error).message}</ErrorBanner>
        </div>
      )}
    </Card>
  );
}

function EditVersion({ version, onClose }: { version: Version; onClose: () => void }) {
  const update = useUpdateVersion();
  const [name, setName] = useState(version.name);
  const [description, setDescription] = useState(version.description ?? "");
  const [startOn, setStartOn] = useState(version.startOn ? dayOf(version.startOn) : "");
  const [releaseOn, setReleaseOn] = useState(version.releaseOn ? dayOf(version.releaseOn) : "");

  function submit(event: FormEvent) {
    event.preventDefault();
    update.mutate(
      { id: version.id, name: name.trim(), description: description.trim(), startOn: startOn || undefined, clearStart: !startOn, releaseOn: releaseOn || undefined, clearRelease: !releaseOn },
      { onSuccess: onClose },
    );
  }

  return (
    <Drawer open onClose={onClose} title={`Edit ${version.name}`} attrs={{ "data-version-editor": version.name }}>
      <form onSubmit={submit} className="space-y-4" noValidate>
        {update.error && <ErrorBanner>{(update.error as Error).message}</ErrorBanner>}
        <Field label="Name" id="field-edit-version-name" required value={name} onChange={(e) => setName(e.target.value)} />
        <Field label="What is in it" id="field-edit-version-description" value={description} onChange={(e) => setDescription(e.target.value)} />
        <div className="grid grid-cols-2 gap-3">
          <Field label="Start on" id="field-edit-version-start" type="date" value={startOn} onChange={(e) => setStartOn(e.target.value)} />
          <Field label="Release on" id="field-edit-version-release" type="date" value={releaseOn} onChange={(e) => setReleaseOn(e.target.value)} />
        </div>
        <Button type="submit" loading={update.isPending} disabled={!name.trim()}>
          Save version
        </Button>
      </form>
    </Drawer>
  );
}

function NotesPanel({ version, onClose }: { version: Version; onClose: () => void }) {
  const { data } = useReleaseNotes(version.id);
  const toast = useToast();
  const notes = data?.notes;
  return (
    <Drawer open onClose={onClose} title={`Release notes for ${version.name}`} attrs={{ "data-release-notes": version.name }}>
      {!notes ? null : notes.groups.length === 0 ? (
        <p className="text-sm text-ink-muted">Nothing is finished for this version yet{notes.open ? `; ${notes.open} open issue${notes.open === 1 ? "" : "s"} name it` : ""}.</p>
      ) : (
        <div className="space-y-4">
          {notes.groups.map((g) => (
            <section key={g.type} data-notes-group={g.type}>
              <h3 className="mb-1 text-xs font-semibold tracking-wide text-ink-muted uppercase">{g.type}</h3>
              <ul className="space-y-1 text-sm">
                {g.issues.map((i) => (
                  <li key={i.key} className="text-ink" data-notes-issue={i.key}>
                    <span className="font-mono text-ink-muted">{i.key}</span> {i.summary}
                  </li>
                ))}
              </ul>
            </section>
          ))}
          {notes.open > 0 && <p className="text-sm text-ink-subtle">{notes.open} more issue{notes.open === 1 ? "" : "s"} name this version and are not done yet.</p>}
          <Button
            variant="secondary"
            size="sm"
            icon={<Icon.Copy />}
            onClick={() => navigator.clipboard.writeText(notesAsText(notes.groups, version.name)).then(() => toast.success("Notes copied"))}
            data-action="copy-notes"
          >
            Copy as text
          </Button>
        </div>
      )}
    </Drawer>
  );
}
