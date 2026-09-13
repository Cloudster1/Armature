import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCloneIssue, useMoveIssue } from "@/api/filters";
import { useStatuses, type Issue } from "@/api/issues";
import { useProjects } from "@/api/projects";
import { Button, Checkbox, Dialog, ErrorBanner, Field, IconButton, Menu, Select, useToast } from "@/components/ui";
import { Icon } from "@/components/icons";

// Copy and move, behind one More button on the issue's head. A copy opens; a
// move stays on the issue, which now has another key.
export function CloneMoveMenu({ issue }: { issue: Issue }) {
  const [cloning, setCloning] = useState(false);
  const [moving, setMoving] = useState(false);
  return (
    <>
      <Menu
        label="More about this issue"
        align="end"
        trigger={(props) => <IconButton icon={<Icon.More />} label="More about this issue" size="sm" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]} data-action="issue-more" />}
        items={[
          { label: "Clone", icon: <Icon.Copy />, onSelect: () => setCloning(true), attrs: { "data-action": "clone" } },
          { label: "Move to another project", icon: <Icon.External />, onSelect: () => setMoving(true), attrs: { "data-action": "move" } },
        ]}
      />
      {cloning && <CloneDialog issue={issue} onClose={() => setCloning(false)} />}
      {moving && <MoveDialog issue={issue} onClose={() => setMoving(false)} />}
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
      title={`Clone ${issue.key}`}
      description="A new issue with the same fields, labels, versions and components; its history starts now."
      attrs={{ "data-clone-dialog": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
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
                    toast.success(`${r.issue.key} made from ${issue.key}`);
                    navigate({ to: "/issues/$issueKey", params: { issueKey: r.issue.key } });
                  },
                },
              )
            }
            data-action="clone-go"
          >
            Clone
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {clone.error && <ErrorBanner>{(clone.error as Error).message}</ErrorBanner>}
        <Field label="Summary" id="field-clone-summary" value={summary} onChange={(e) => setSummary(e.target.value)} />
        <Checkbox label="Copy its links" checked={links} onChange={(e) => setLinks(e.target.checked)} id="field-clone-links" />
        <Checkbox label="Copy its subtasks" checked={subtasks} onChange={(e) => setSubtasks(e.target.checked)} id="field-clone-subtasks" />
      </div>
    </Dialog>
  );
}

function MoveDialog({ issue, onClose }: { issue: Issue; onClose: () => void }) {
  const move = useMoveIssue();
  const navigate = useNavigate();
  const toast = useToast();
  const { data: projects } = useProjects();
  const { data: statuses } = useStatuses();
  const [projectKey, setProjectKey] = useState("");
  const [statusId, setStatusId] = useState("");
  const targets = (projects?.projects ?? []).filter((p) => p.key !== issue.projectKey);
  const needsStatus = (move.error as Error | null)?.message.includes("choose one") ?? false;
  return (
    <Dialog
      open
      onClose={onClose}
      title={`Move ${issue.key}`}
      description="The issue keeps its history, comments and files, gets the other project's next key, and its old address still finds it. Sprint, team, milestone, versions and components stay behind."
      attrs={{ "data-move-dialog": "" }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
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
                    toast.success(`${issue.key} is now ${r.issue.key}`);
                    navigate({ to: "/issues/$issueKey", params: { issueKey: r.issue.key } });
                  },
                },
              )
            }
            data-action="move-go"
          >
            Move
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {move.error && <ErrorBanner>{(move.error as Error).message}</ErrorBanner>}
        <Select label="Project" id="field-move-project" value={projectKey} onChange={(e) => setProjectKey(e.target.value)}>
          <option value="">Choose a project</option>
          {targets.map((p) => (
            <option key={p.key} value={p.key}>
              {p.name} ({p.key})
            </option>
          ))}
        </Select>
        {(needsStatus || statusId) && (
          <Select label="Status there" id="field-move-status" value={statusId} onChange={(e) => setStatusId(e.target.value)} hint="The other project's workflow does not have this issue's status; choose one it has.">
            <option value="">Choose a status</option>
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
