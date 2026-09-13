import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { Placement } from "@/api/arrange";
import type { Field } from "@/api/fields";

// The create form asks for what the arrangement placed, in the order the page
// draws it, and says what would not stick after the issue was made.

const nothing = () => ({ data: undefined, isLoading: false, error: null });

const SEVERITY = "f-severity";

const state = {
  places: [] as Placement[],
  created: vi.fn(),
  request: vi.fn(),
};

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useNavigate: () => vi.fn(),
}));

vi.mock("@/api/client", () => ({
  request: (path: string, init: { method: string; body: unknown }) => state.request(path, init),
}));

vi.mock("@/api/issues", async () => ({
  ...(await vi.importActual<typeof import("@/api/issues")>("@/api/issues")),
  useCreateIssue: () => ({ mutate: state.created, isPending: false, error: null, reset: vi.fn() }),
  useIssueTypes: () => ({
    data: {
      issueTypes: [
        { id: "t-epic", name: "Epic", icon: "epic", level: 1, isSubtask: false },
        { id: "t-story", name: "Story", icon: "story", level: 0, isSubtask: false },
      ],
    },
    isLoading: false,
  }),
  useMembers: () => ({ data: { members: [{ id: "u1", name: "Ada", role: "member" }] }, isLoading: false }),
}));

vi.mock("@/api/projects", async () => ({
  ...(await vi.importActual<typeof import("@/api/projects")>("@/api/projects")),
  useProjects: () => ({ data: { projects: [{ key: "CP", name: "Customer Portal" }] }, isLoading: false }),
}));

vi.mock("@/api/arrange", async () => ({
  ...(await vi.importActual<typeof import("@/api/arrange")>("@/api/arrange")),
  useArrangements: () => ({
    data: { arrangements: [{ issueTypeId: "t-story", issueTypeName: "Story", origin: { scope: "builtin", named: false }, places: state.places }] },
    isLoading: false,
  }),
}));

const severity: Field = { id: SEVERITY, projectId: "p1", projectKey: "CP", org: false, name: "Severity", kind: "select", options: ["Low", "High"], position: 0, createdAt: "", updatedAt: "" };

vi.mock("@/api/fields", async () => ({
  ...(await vi.importActual<typeof import("@/api/fields")>("@/api/fields")),
  useProjectFields: () => ({ data: { fields: [severity] }, isLoading: false }),
}));

vi.mock("@/api/sprints", async () => ({
  ...(await vi.importActual<typeof import("@/api/sprints")>("@/api/sprints")),
  useSprints: () => ({ data: { sprints: [{ id: "sp1", name: "Sprint 4", state: "active" }] }, isLoading: false }),
}));

vi.mock("@/api/milestones", async () => ({
  ...(await vi.importActual<typeof import("@/api/milestones")>("@/api/milestones")),
  useMilestones: () => ({ data: { milestones: [{ id: "m1", name: "Launch" }] }, isLoading: false }),
}));

vi.mock("@/api/teams", async () => ({
  ...(await vi.importActual<typeof import("@/api/teams")>("@/api/teams")),
  useTeams: nothing,
}));

vi.mock("@/api/versions", async () => ({
  ...(await vi.importActual<typeof import("@/api/versions")>("@/api/versions")),
  useVersions: () => ({ data: { versions: [{ id: "v1", name: "1.2" }] }, isLoading: false }),
  useComponents: nothing,
}));

vi.mock("@/api/labels", async () => ({
  ...(await vi.importActual<typeof import("@/api/labels")>("@/api/labels")),
  useLabels: () => ({ data: { labels: [{ name: "regression" }] }, isLoading: false }),
}));

const { ToastProvider } = await import("@/components/ui");
const { CreateIssueDialog } = await import("./CreateIssueDialog");

const builtIn: Placement[] = [
  { area: "main", slot: "description" },
  { area: "people", slot: "assignee" },
  { area: "people", slot: "reporter" },
  { area: "planning", slot: "sprint" },
  { area: "planning", slot: "milestone" },
  { area: "tracking", slot: "goals" },
  { area: "tracking", slot: "priority" },
  { area: "tracking", slot: "labels" },
  { area: "more", slot: "otherFields" },
  { area: "more", slot: "created" },
];

function draw(places: Placement[] = builtIn) {
  state.places = places;
  return render(
    <ToastProvider>
      <CreateIssueDialog open onClose={() => {}} projectKey="CP" />
    </ToastProvider>,
  );
}

/** Every control the form asks for, by the label a person reads. */
function asked(): string[] {
  const form = document.getElementById("create-issue");
  if (!form) throw new Error("no form");
  return [...form.querySelectorAll("label, [role='group'] > span, span[id$='-label']")].map((el) => el.textContent?.trim() ?? "").filter((text) => text !== "");
}

describe("CreateIssueDialog", () => {
  beforeEach(() => {
    state.created = vi.fn();
    state.request = vi.fn().mockResolvedValue({});
  });

  it("asks for what the arrangement placed, in the order the page draws it", () => {
    draw();
    const labels = asked();
    expect(labels.slice(0, 2)).toEqual(["Summary", "Type"]);
    expect(labels.slice(2)).toEqual([
      "Description",
      "Assignee",
      "Reporter",
      "Sprint",
      "Milestone",
      "Priority",
      "Labels",
      "Severity",
    ]);
  });

  // A field nobody shows is not a field anybody is asked for.
  it("does not ask about a slot the arrangement hid", () => {
    draw([
      { area: "people", slot: "assignee" },
      { area: "hidden", slot: "sprint" },
    ]);
    expect(screen.getByLabelText("Assignee")).toBeInTheDocument();
    expect(screen.queryByLabelText("Sprint")).not.toBeInTheDocument();
  });

  // Goals are a clock the desk starts and Created is a fact about an issue
  // that exists; both are placed on the page and neither can be answered here.
  it("draws nothing for a slot with nothing to ask", () => {
    draw([
      { area: "tracking", slot: "goals" },
      { area: "more", slot: "created" },
    ]);
    expect(asked()).toEqual(["Summary", "Type"]);
  });

  // Without a type the form cannot know which arrangement to follow, so it
  // names the one the server would have chosen rather than leaving it blank.
  it("names a type rather than letting the server pick", () => {
    draw();
    expect(screen.getByLabelText("Type")).toHaveValue("t-story");
    expect(screen.queryByRole("option", { name: "The project's default" })).not.toBeInTheDocument();
  });

  it("sends what the create request can carry", async () => {
    draw();
    await userEvent.type(screen.getByLabelText("Summary"), "Login loops");
    await userEvent.selectOptions(screen.getByLabelText("Assignee"), "u1");
    await userEvent.selectOptions(screen.getByLabelText("Sprint"), "sp1");
    await userEvent.click(screen.getByRole("button", { name: "Create issue" }));

    expect(state.created).toHaveBeenCalledTimes(1);
    expect(state.created.mock.calls[0]?.[0]).toEqual({ projectKey: "CP", summary: "Login loops", typeId: "t-story", assigneeId: "u1", sprintId: "sp1" });
  });

  // The rest has an endpoint of its own, so it follows the issue rather than
  // holding it up, and it is one request per thing.
  it("applies what the create request cannot carry", async () => {
    draw();
    state.created.mockImplementation((_body: unknown, opts: { onSuccess: (r: { issue: { key: string } }) => void }) => opts.onSuccess({ issue: { key: "CP-7" } }));

    await userEvent.type(screen.getByLabelText("Summary"), "Login loops");
    await userEvent.selectOptions(screen.getByLabelText("Reporter"), "u1");
    await userEvent.selectOptions(screen.getByLabelText("Milestone"), "m1");
    await userEvent.selectOptions(screen.getByLabelText("Severity"), "High");
    await userEvent.click(within(screen.getByRole("group", { name: "Labels already in use" })).getByRole("button", { name: "regression" }));
    await userEvent.click(screen.getByRole("button", { name: "Create issue" }));

    await waitFor(() => expect(state.request).toHaveBeenCalledTimes(4));
    const paths = state.request.mock.calls.map((call) => call[0]);
    expect(paths).toEqual(["/issues/CP-7", "/issues/CP-7/labels", "/issues/CP-7/milestone", `/issues/CP-7/fields/${SEVERITY}`]);
    expect(state.request.mock.calls[0]?.[1]).toMatchObject({ method: "PATCH", body: { reporterId: "u1" } });
    expect(state.request.mock.calls[1]?.[1]).toMatchObject({ body: { labels: ["regression"] } });
    expect(state.request.mock.calls[3]?.[1]).toMatchObject({ body: { value: "High" } });
  });

  // The issue exists; saying nothing would lose the rest silently. The issue
  // also cannot be made a second time by pressing the button again.
  it("names the issue it made, says what did not stick, and will not make it twice", async () => {
    draw();
    state.created.mockImplementation((_body: unknown, opts: { onSuccess: (r: { issue: { key: string } }) => void }) => opts.onSuccess({ issue: { key: "CP-8" } }));
    state.request = vi.fn().mockRejectedValue(new Error("Launch has already been reached"));

    await userEvent.type(screen.getByLabelText("Summary"), "Login loops");
    await userEvent.selectOptions(screen.getByLabelText("Milestone"), "m1");
    await userEvent.click(screen.getByRole("button", { name: "Create issue" }));

    const said = await screen.findByText(/did not stick/);
    expect(said).toHaveTextContent("CP-8 was created, but this did not stick: Milestone (Launch has already been reached)");
    expect(screen.getByRole("link", { name: "Open CP-8" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create issue" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Create and open" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Cancel" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Done" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Create issue" }));
    expect(state.created).toHaveBeenCalledTimes(1);
  });
});
