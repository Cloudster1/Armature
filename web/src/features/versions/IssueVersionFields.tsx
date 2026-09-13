import { useState } from "react";
import type { Issue } from "@/api/issues";
import { useComponents, useSetIssueComponents, useSetIssueVersions, useVersions } from "@/api/versions";
import { Button, Checkbox, Popover, Tag } from "@/components/ui";
import { Icon } from "@/components/icons";

// A set of names picked from a list: the tags show what is chosen, a popover
// with checkboxes changes it, and the whole set is sent at once.
function NamePicker({
  id,
  label,
  chosen,
  options,
  editable,
  onChange,
  emptyWord,
}: {
  id: string;
  label: string;
  chosen: Array<{ id: string; name: string }>;
  options: Array<{ id: string; name: string }>;
  editable: boolean;
  onChange: (ids: string[]) => void;
  emptyWord: string;
}) {
  const [open, setOpen] = useState(false);
  const chosenIDs = new Set(chosen.map((c) => c.id));
  // Something chosen that the list no longer offers (archived) still shows.
  const all = [...options, ...chosen.filter((c) => !options.some((o) => o.id === c.id))];
  return (
    <span className="flex flex-wrap items-center justify-end gap-1" data-picker={id}>
      {chosen.length === 0 && <span className="text-sm text-ink-subtle">{emptyWord}</span>}
      {chosen.map((c) => (
        <Tag key={c.id} data-picked={c.name}>
          {c.name}
        </Tag>
      ))}
      {editable && (
        <Popover
          open={open}
          onClose={() => setOpen(false)}
          label={label}
          align="end"
          trigger={<Button variant="ghost" size="sm" icon={<Icon.Edit />} onClick={() => setOpen((o) => !o)} aria-label={`Change ${label.toLowerCase()}`} id={id} />}
        >
          {all.length === 0 ? (
            <p className="text-sm text-ink-muted">Nothing to choose from yet.</p>
          ) : (
            <div className="flex flex-col gap-1.5" role="group" aria-label={label}>
              {all.map((o) => (
                <Checkbox
                  key={o.id}
                  label={o.name}
                  checked={chosenIDs.has(o.id)}
                  onChange={(e) => onChange(e.target.checked ? [...chosenIDs, o.id] : [...chosenIDs].filter((x) => x !== o.id))}
                  data-picker-option={o.name}
                />
              ))}
            </div>
          )}
        </Popover>
      )}
    </span>
  );
}

/** One of the two version sets on an issue: what ships it, or where it was found. */
export function VersionField({ issue, editable, role }: { issue: Issue; editable: boolean; role: "fix" | "affects" }) {
  const { data } = useVersions(issue.projectKey);
  const set = useSetIssueVersions();
  const versions = data?.versions ?? [];
  const fix = issue.fixVersions ?? [];
  const affects = issue.affectsVersions ?? [];
  const chosen = role === "fix" ? fix : affects;
  const change = (ids: string[]) =>
    set.mutate({ key: issue.key, fix: role === "fix" ? ids : fix.map((f) => f.id), affects: role === "affects" ? ids : affects.map((a) => a.id) });
  return (
    <>
      <NamePicker id={role === "fix" ? "issue-fix-versions" : "issue-affects-versions"} label={role === "fix" ? "Fix versions" : "Affects versions"} chosen={chosen} options={versions} editable={editable} emptyWord="None" onChange={change} />
      {set.error && <span className="block text-right text-2xs text-danger">{(set.error as Error).message}</span>}
    </>
  );
}

/** The project's parts the issue belongs to. */
export function ComponentField({ issue, editable }: { issue: Issue; editable: boolean }) {
  const { data } = useComponents(issue.projectKey);
  const set = useSetIssueComponents();
  return (
    <>
      <NamePicker id="issue-components" label="Components" chosen={issue.components ?? []} options={data?.components ?? []} editable={editable} emptyWord="None" onChange={(ids) => set.mutate({ key: issue.key, components: ids })} />
      {set.error && <span className="block text-right text-2xs text-danger">{(set.error as Error).message}</span>}
    </>
  );
}
