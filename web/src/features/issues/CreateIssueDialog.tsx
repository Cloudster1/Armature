import { Fragment, useEffect, useMemo, useState, type FormEvent } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useArrangements } from "@/api/arrange";
import { LEVEL_STANDARD, useCreateIssue, useIssueTypes, type Issue } from "@/api/issues";
import { useProjects } from "@/api/projects";
import { Button, Dialog, ErrorBanner, Field, Select, Skeleton, useToast } from "@/components/ui";
import { DIALOG_SKELETON_LINES } from "@/config";
import { namedFields } from "./view/areas";
import { applyRest, type Leftover } from "./create/apply";
import { askedPlaces, createBody, emptyDraft, type Draft } from "./create/draft";
import { renderAsk } from "./create/slots";

// One dialog for making an issue from anywhere. It asks for what the project
// arranged for this issue type, in the order the issue page draws it.
export function CreateIssueDialog({ open, onClose, projectKey }: { open: boolean; onClose: () => void; projectKey?: string }) {
  const create = useCreateIssue();
  const toast = useToast();
  const navigate = useNavigate();
  const { data: projectData } = useProjects();
  const { data: typeData } = useIssueTypes();
  const [project, setProject] = useState(projectKey ?? "");
  const [typeId, setTypeId] = useState("");
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [refused, setRefused] = useState<Leftover[]>([]);
  // The issue that was made but did not take everything. It exists, so the
  // dialog must not offer to make it again.
  const [made, setMade] = useState<Issue | null>(null);
  const { data: arranged } = useArrangements(project);

  const projects = projectData?.projects ?? [];
  // Types below the standard level only exist underneath another issue, so
  // they are made from that issue's children panel.
  const types = (typeData?.issueTypes ?? []).filter((type) => type.level >= LEVEL_STANDARD);
  // The type is never left to the server: without one the form cannot know
  // which arrangement to follow. This is the type the server would have picked.
  const fallbackType = types.find((type) => type.level === LEVEL_STANDARD)?.id ?? types[0]?.id ?? "";
  const chosenType = typeId || fallbackType;
  const arrangement = arranged?.arrangements.find((each) => each.issueTypeId === chosenType);
  const places = useMemo(() => askedPlaces(arrangement?.places ?? []), [arrangement]);
  const named = namedFields(arrangement?.places ?? []);

  useEffect(() => {
    if (!open) return;
    setProject(projectKey ?? projects[0]?.key ?? "");
    setDraft(emptyDraft());
    setRefused([]);
    setMade(null);
    create.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, projectKey]);

  const set = (patch: Partial<Draft>) => setDraft((was) => ({ ...was, ...patch }));
  const ready = made === null && draft.summary.trim() !== "" && project !== "" && chosenType !== "";

  function land(issue: Issue, left: Leftover[], andOpen: boolean) {
    if (left.length > 0) {
      setMade(issue);
      setRefused(left);
      return;
    }
    onClose();
    if (andOpen) {
      void navigate({ to: "/issues/$issueKey", params: { issueKey: issue.key } });
      return;
    }
    toast.success(`Created ${issue.key}`, {
      link: (
        <Link to="/issues/$issueKey" params={{ issueKey: issue.key }} className="font-medium text-accent hover:underline">
          Open
        </Link>
      ),
    });
  }

  function submit(andOpen: boolean) {
    if (!ready) return;
    setRefused([]);
    create.mutate(createBody(draft, project, chosenType), {
      onSuccess: ({ issue }) => {
        void applyRest(issue.key, draft).then((left) => land(issue, left, andOpen));
      },
    });
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    submit(false);
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="New issue"
      attrs={{ "data-create-issue": "" }}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {made ? "Done" : "Cancel"}
          </Button>
          <Button variant="secondary" onClick={() => submit(true)} loading={create.isPending} disabled={!ready}>
            Create and open
          </Button>
          <Button type="submit" form="create-issue" loading={create.isPending} disabled={!ready}>
            Create issue
          </Button>
        </>
      }
    >
      <form id="create-issue" onSubmit={onSubmit} className="space-y-3" noValidate>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
        {made && refused.length > 0 && <Refused issueKey={made.key} left={refused} />}
        {!projectKey && (
          <Select label="Project" value={project} onChange={(e) => setProject(e.target.value)}>
            {projects.map((p) => (
              <option key={p.key} value={p.key}>
                {p.name} ({p.key})
              </option>
            ))}
          </Select>
        )}
        <Field label="Summary" value={draft.summary} onChange={(e) => set({ summary: e.target.value })} autoFocus required placeholder="What needs doing" />
        <Select label="Type" id="issue-type" aria-label="Issue type" value={chosenType} onChange={(e) => setTypeId(e.target.value)}>
          {types.map((type) => (
            <option key={type.id} value={type.id}>
              {type.name}
            </option>
          ))}
        </Select>
        {arrangement ? (
          places.map((place) => <Fragment key={place.fieldId ?? place.slot}>{renderAsk(place, { draft, set, projectKey: project, named })}</Fragment>)
        ) : (
          <Skeleton lines={DIALOG_SKELETON_LINES} />
        )}
      </form>
    </Dialog>
  );
}

// The issue exists, so the buttons that would make it are dead and the key is
// on the screen: nothing is quietly lost, which is what the CSV import does.
function Refused({ issueKey, left }: { issueKey: string; left: Leftover[] }) {
  return (
    <ErrorBanner>
      <span data-create-refused={issueKey}>
        {issueKey} was created, but this did not stick: {left.map((each) => `${each.what} (${each.why})`).join("; ")}. Close this and set it on the issue.
      </span>{" "}
      <Link to="/issues/$issueKey" params={{ issueKey }} className="font-medium underline">
        Open {issueKey}
      </Link>
    </ErrorBanner>
  );
}
