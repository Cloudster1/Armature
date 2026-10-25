import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Rule, RuleDescriptor, RuleInput } from "@/api/automation";

vi.mock("@/features/labels/LabelInput", () => ({ LabelInput: () => null }));

const { RuleEditor } = await import("./RuleEditor");

const catalog: RuleDescriptor[] = [
  { kind: "trigger", type: "issue.created", label: "Issue created", description: "", options: [] },
  { kind: "trigger", type: "scheduled", label: "On a schedule", description: "", options: [] },
  { kind: "action", type: "add_label", label: "Add a label", description: "", options: [] },
];

function weeklyRule(weekday?: number): Rule {
  return {
    id: "r1",
    name: "Weekly sweep",
    enabled: true,
    trigger: { kind: "scheduled", query: "status = Open", schedule: { unit: "weekly", every: 1, at: "09:00", ...(weekday === undefined ? {} : { weekday }) } },
    conditions: [],
    actions: [{ kind: "add_label", value: "swept" }],
    allowOwnEvents: false,
    hourlyCap: 0,
    createdAt: "2026-10-01T00:00:00Z",
    updatedAt: "2026-10-01T00:00:00Z",
  };
}

function editor(rule: Rule | null, onSave: (input: RuleInput) => void) {
  return render(<RuleEditor open onClose={() => {}} rule={rule} catalog={catalog} webhooks={[]} onSave={onSave} saving={false} />);
}

function weekdaySelect(): HTMLSelectElement {
  return document.querySelector<HTMLSelectElement>("#field-weekday")!;
}

describe("a weekly rule's day", () => {
  // The select showed Monday while nothing was set, and the server read the
  // missing day as Sunday.
  it("is saved as the day the select showed when nobody touched it", async () => {
    const onSave = vi.fn();
    editor(null, onSave);
    await userEvent.type(screen.getByLabelText(/Rule name/), "Monday sweep");
    await userEvent.selectOptions(document.querySelector<HTMLSelectElement>("#field-trigger")!, "scheduled");
    await userEvent.type(screen.getByLabelText(/Query/), "status = Open");
    await userEvent.selectOptions(document.querySelector<HTMLSelectElement>("#field-rhythm")!, "weekly");

    const shown = weekdaySelect();
    expect(shown.selectedOptions[0]?.textContent).toBe("Monday");
    await userEvent.click(screen.getByRole("button", { name: "Create rule" }));

    expect(onSave).toHaveBeenCalledTimes(1);
    const saved = onSave.mock.calls[0]![0] as RuleInput;
    expect(saved.trigger.schedule).toMatchObject({ unit: "weekly", weekday: Number(shown.value) });
  });

  it("reads Sunday for a rule saved with weekday 0", () => {
    editor(weeklyRule(0), () => {});
    expect(weekdaySelect().selectedOptions[0]?.textContent).toBe("Sunday");
  });

  // The server leaves a weekday of 0 out of its answer, and runs such a rule on Sunday.
  it("reads Sunday for a rule whose answer leaves the day out", () => {
    editor(weeklyRule(), () => {});
    expect(weekdaySelect().selectedOptions[0]?.textContent).toBe("Sunday");
  });

  it("keeps a day already picked when the rhythm is switched away and back", async () => {
    const onSave = vi.fn();
    editor(weeklyRule(4), onSave);
    await userEvent.selectOptions(document.querySelector<HTMLSelectElement>("#field-rhythm")!, "daily");
    await userEvent.selectOptions(document.querySelector<HTMLSelectElement>("#field-rhythm")!, "weekly");
    expect(weekdaySelect().selectedOptions[0]?.textContent).toBe("Thursday");
  });
});
