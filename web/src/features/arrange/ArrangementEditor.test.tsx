import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Arrangement, Placement } from "@/api/arrange";
import type { Field } from "@/api/fields";
import { ArrangementEditor } from "./ArrangementEditor";

const BUG_TYPE = "11111111-1111-1111-1111-111111111111";
const STORY_TYPE = "22222222-2222-2222-2222-222222222222";
const SEVERITY = "33333333-3333-3333-3333-333333333333";

function arrangement(over: Partial<Arrangement> = {}): Arrangement {
  return {
    issueTypeId: BUG_TYPE,
    issueTypeName: "Bug",
    origin: { scope: "project", named: true },
    places: [
      { area: "people", slot: "assignee" },
      { area: "people", slot: "reporter" },
      { area: "planning", slot: "sprint" },
      { area: "more", fieldId: SEVERITY },
    ],
    ...over,
  };
}

function field(over: Partial<Field> = {}): Field {
  return {
    id: SEVERITY,
    projectId: "p",
    projectKey: "CP",
    org: false,
    name: "Severity",
    kind: "select",
    options: ["Low", "High"],
    position: 0,
    createdAt: "",
    updatedAt: "",
    ...over,
  };
}

function draw(over: Partial<Parameters<typeof ArrangementEditor>[0]> = {}) {
  const onSave = vi.fn();
  const view = render(
    <ArrangementEditor
      arrangements={[arrangement()]}
      fields={[field()]}
      saving={false}
      error={null}
      canFollow
      onSave={onSave}
      {...over}
    />,
  );
  return { ...view, onSave };
}

function place(container: HTMLElement, key: string) {
  const row = container.querySelector(`[data-arrange-place="${key}"]`);
  if (!(row instanceof HTMLElement)) throw new Error(`no place for ${key}`);
  return within(row);
}

describe("ArrangementEditor", () => {
  it("draws every part of the page, including the one that hides a field", () => {
    draw();
    for (const title of ["Beside the description", "People", "Planning", "Tracking", "Fields", "Not shown"]) {
      expect(screen.getByRole("heading", { name: title })).toBeInTheDocument();
    }
  });

  // A slot carries this tracker's word for it; a custom field carries the
  // project's own name, which is the only way to tell two of them apart.
  it("names a slot and names a project's field", () => {
    const { container } = draw();
    expect(place(container, "assignee").getByText("Assignee")).toBeInTheDocument();
    expect(place(container, SEVERITY).getByText("Severity")).toBeInTheDocument();
  });

  it("says who decided the arrangement being looked at", () => {
    draw();
    expect(screen.getByText("This project's own")).toBeInTheDocument();
  });

  // The buttons exist so the keyboard can do what the mouse does by dragging.
  it("sends the whole arrangement with two places swapped", async () => {
    const { container, onSave } = draw();
    await userEvent.click(place(container, "assignee").getByRole("button", { name: "Down" }));

    expect(onSave).toHaveBeenCalledTimes(1);
    const sent = onSave.mock.calls[0]?.[0] as { issueTypeId: string; places: Placement[] };
    expect(sent.issueTypeId).toBe(BUG_TYPE);
    expect(sent.places.filter((each) => each.area === "people").map((each) => each.slot)).toEqual(["reporter", "assignee"]);
    expect(sent.places).toHaveLength(4);
  });

  it("leaves the first place of an area with nowhere earlier to go", () => {
    const { container } = draw();
    expect(place(container, "assignee").getByRole("button", { name: "Up" })).toBeDisabled();
    expect(place(container, "reporter").getByRole("button", { name: "Down" })).toBeDisabled();
  });

  it("moves a place to the part of the page that was chosen", async () => {
    const { container, onSave } = draw();
    await userEvent.selectOptions(place(container, "sprint").getByRole("combobox"), "hidden");

    const sent = onSave.mock.calls[0]?.[0] as { places: Placement[] };
    expect(sent.places).toHaveLength(4);
    expect(sent.places.find((each) => each.slot === "sprint")?.area).toBe("hidden");
  });

  it("hands a type back to the organization with no places at all", async () => {
    const { onSave } = draw();
    await userEvent.click(screen.getByRole("button", { name: "Follow the organization" }));

    expect(onSave).toHaveBeenCalledWith({ issueTypeId: BUG_TYPE, places: null });
  });

  // Nothing to hand back to: the organization is the top of the chain, and a
  // project that already follows it has nothing of its own to drop.
  it("does not offer to follow what cannot be followed", () => {
    draw({ canFollow: false });
    expect(screen.queryByRole("button", { name: "Follow the organization" })).not.toBeInTheDocument();

    draw({ arrangements: [arrangement({ origin: { scope: "organization", named: false } })] });
    expect(screen.queryByRole("button", { name: "Follow the organization" })).not.toBeInTheDocument();
  });

  it("arranges the type that was chosen", async () => {
    const story = arrangement({
      issueTypeId: STORY_TYPE,
      issueTypeName: "Story",
      places: [{ area: "tracking", slot: "estimate" }],
    });
    const { container, onSave } = draw({ arrangements: [arrangement(), story] });

    await userEvent.selectOptions(screen.getByLabelText("Issue type"), STORY_TYPE);
    expect(container.querySelector('[data-arrange-place="assignee"]')).toBeNull();

    await userEvent.selectOptions(place(container, "estimate").getByRole("combobox"), "people");
    expect(onSave).toHaveBeenCalledWith({ issueTypeId: STORY_TYPE, places: [{ area: "people", slot: "estimate" }] });
  });

  it("says what the server refused", () => {
    draw({ error: new Error("Severity is placed twice; a field is in one place") });
    expect(screen.getByText("Severity is placed twice; a field is in one place")).toBeInTheDocument();
  });
});
