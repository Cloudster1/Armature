import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCloneIssue, useMoveIssue } from "@/api/filters";
import { useIssueFields, type FieldValue } from "@/api/fields";
import { useStatuses, type Issue } from "@/api/issues";
import { Button, Checkbox, Dialog, ErrorBanner, Field, IconButton, Menu, Select, useToast, type MenuItem } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ExportDialog, keysQuery } from "./ExportDialog";
import { ProjectPicker } from "@/features/projects/ProjectPicker";
import { t } from "@/i18n";

// Copy, move and export behind one More button on the issue's head; a reader
// is offered only the export. A copy opens; a moved issue gets another key.
export function CloneMoveMenu({ issue, editable }: { issue: Issue; editable: boolean }) {
  const [cloning, setCloning] = useState(false);
  const [moving, setMoving] = useState(false);
  const [exporting, setExporting] = useState(false);
  const changes: MenuItem[] = editable
    ? [
        { label: t.search.issueMenu.clone, icon: <Icon.Copy />, onSelect: () => setCloning(true), attrs: { "data-action": "clone" } },
        { label: t.search.issueMenu.move, icon: <Icon.External />, onSelect: () => setMoving(true), attrs: { "data-action": "move" } },
      ]
    : [];
  return (
    <>
      <Menu
        label={t.search.issueMenu.more}
        align="end"
        trigger={(props) => (
          <IconButton
            icon={<Icon.More />}
            label={t.search.issueMenu.more}
            size="sm"
            onClick={props.toggle}
            aria-haspopup={props["aria-haspopup"]}
            aria-expanded={props["aria-expanded"]}
            data-action="issue-more"
          />
        )}
        items={[
          ...changes,
          { label: t.search.issueMenu.exportCsv, icon: <Icon.Download />, onSelect: () => setExporting(true), attrs: { "data-action": "export-issue" } },
        ]}
      />
      {cloning && <CloneDialog issue={issue} onClose={() => setCloning(false)} />}
      {moving && <MoveDialog issue={issue} onClose={() => setMoving(false)} />}
      <ExportDialog query={keysQuery([issue.key])} open={exporting} onClose={() => setExporting(false)} description={t.search.issueMenu.exportOne(issue.key)} />
    </>
  );
}

function CloneDialog({ issue, onClose }: { issue: Issue; onClose: () => void }) {
  const clone = useCloneIssue();
  const navigate = useNavigate();
  const toast = useToast();
  const [summary, setSummary] = useState(issue.summary);
  const [links, setLinks] = useState(true);
  const [subtasks, setSubtasks] = useState(false);
  return (
    <Dialog
      open
      onClose={onClose}
      title={t.search.issueMenu.cloneTitle(issue.key)}
      description={t.search.issueMenu.cloneDescription}
      attrs={{ "data-clone-dialog": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t.search.issueMenu.cancel}
          </Button>
          <Button
            loading={clone.isPending}
            disabled={!summary.trim()}
            onClick={() =>
              clone.mutate(
                { key: issue.key, links, subtasks, summary: summary.trim() },
                {
                  onSuccess: (r) => {
                    onClose();
                    toast.success(t.search.issueMenu.cloned(r.issue.key, issue.key));
                    navigate({ to: "/issues/$issueKey", params: { issueKey: r.issue.key } });
                  },
                },
              )
            }
            data-action="clone-go"
          >
            {t.search.issueMenu.clone}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {clone.error && <ErrorBanner>{(clone.error as Error).message}</ErrorBanner>}
        <Field label={t.search.issueMenu.summary} id="field-clone-summary" value={summary} onChange={(e) => setSummary(e.target.value)} />
        <Checkbox label={t.search.issueMenu.copyLinks} checked={links} onChange={(e) => setLinks(e.target.checked)} id="field-clone-links" />
        <Checkbox label={t.search.issueMenu.copySubtasks} checked={subtasks} onChange={(e) => setSubtasks(e.target.checked)} id="field-clone-subtasks" />
      </div>
    </Dialog>
  );
}

/** The answers a move leaves behind: those to the old project's own fields. */
export function droppedOnMove(values: FieldValue[]): FieldValue[] {
  return values.filter((v) => !v.field.org && v.value !== undefined && v.value !== null);
}

export function MoveDialog({ issue, onClose }: { issue: Issue; onClose: () => void }) {
  const move = useMoveIssue();
  const navigate = useNavigate();
  const toast = useToast();
  const { data: statuses } = useStatuses();
  const { data: fields } = useIssueFields(issue.key);
  const dropped = droppedOnMove(fields?.values ?? []);
  const [projectKey, setProjectKey] = useState("");
  const [statusId, setStatusId] = useState("");
  const needsStatus = (move.error as Error | null)?.message.includes("choose one") ?? false;
  return (
    <Dialog
      open
      onClose={onClose}
      title={t.search.issueMenu.moveTitle(issue.key)}
      description={t.search.issueMenu.moveDescription}
      attrs={{ "data-move-dialog": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t.search.issueMenu.cancel}
          </Button>
          <Button
            loading={move.isPending}
            disabled={!projectKey}
            onClick={() =>
              move.mutate(
                { key: issue.key, projectKey, statusId: statusId || undefined },
                {
                  onSuccess: (r) => {
                    onClose();
                    toast.success(t.search.issueMenu.moved(issue.key, r.issue.key));
                    navigate({ to: "/issues/$issueKey", params: { issueKey: r.issue.key } });
                  },
                },
              )
            }
            data-action="move-go"
          >
            {t.search.issueMenu.moveButton}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {move.error && <ErrorBanner>{(move.error as Error).message}</ErrorBanner>}
        <ProjectPicker label={t.search.issueMenu.project} id="field-move-project" value={projectKey} onChange={setProjectKey} exclude={[issue.projectKey]} />
        {dropped.length > 0 && (
          <div className="space-y-1 text-sm" data-move-dropped="">
            <p className="text-ink-muted">{t.search.issueMenu.dropped(issue.projectKey)}</p>
            <ul className="list-disc pl-5">
              {dropped.map((v) => (
                <li key={v.field.id}>
                  {v.field.name}: {v.display ?? String(v.value)}
                </li>
              ))}
            </ul>
          </div>
        )}
        {(needsStatus || statusId) && (
          <Select
            label={t.search.issueMenu.statusThere}
            id="field-move-status"
            value={statusId}
            onChange={(e) => setStatusId(e.target.value)}
            hint={t.search.issueMenu.statusHint}
          >
            <option value="">{t.search.issueMenu.chooseStatus}</option>
            {(statuses?.statuses ?? []).map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        )}
      </div>
    </Dialog>
  );
}
