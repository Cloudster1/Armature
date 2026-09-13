import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { Button, Chip, EmptyState, ErrorBanner, Field, OptionCard, SelectInput, cx } from "./index";

describe("cx", () => {
  it("joins the truthy parts only", () => {
    expect(cx("a", false, null, undefined, "b")).toBe("a b");
    expect(cx()).toBe("");
  });
});

describe("Button", () => {
  it("is disabled and marked busy while loading", () => {
    render(<Button loading>Save</Button>);
    const button = screen.getByRole("button", { name: "Save" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("aria-busy", "true");
  });

  // The label has to survive the loading state, or a screen reader loses track
  // of which button it is on.
  it("keeps its accessible name while loading", () => {
    const { rerender } = render(<Button>Save</Button>);
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
    rerender(<Button loading>Save</Button>);
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  it("does not fire while loading", async () => {
    const onClick = vi.fn();
    render(
      <Button loading onClick={onClick}>
        Save
      </Button>,
    );
    await userEvent.click(screen.getByRole("button", { name: "Save" })).catch(() => {});
    expect(onClick).not.toHaveBeenCalled();
  });

  it("fires when idle", async () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>Save</Button>);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});

describe("Field", () => {
  it("associates its label with its input", () => {
    render(<Field label="Work email" />);
    expect(screen.getByLabelText("Work email")).toBeInTheDocument();
  });

  it("describes the input by its hint", () => {
    render(<Field label="Password" hint="At least 12 characters." />);
    const input = screen.getByLabelText("Password");
    expect(input).toHaveAccessibleDescription("At least 12 characters.");
    expect(input).not.toHaveAttribute("aria-invalid");
  });

  it("describes the input by its error, and marks it invalid", () => {
    render(<Field label="Email" error="That address is not valid." />);
    const input = screen.getByLabelText("Email");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAccessibleDescription("That address is not valid.");
  });

  // An error must replace the hint rather than sit alongside it, otherwise the
  // description read aloud contains both and the problem gets buried.
  it("prefers the error over the hint", () => {
    render(<Field label="Email" hint="Your work address." error="Required." />);
    expect(screen.getByLabelText("Email")).toHaveAccessibleDescription("Required.");
    expect(screen.queryByText("Your work address.")).not.toBeInTheDocument();
  });
});

describe("ErrorBanner", () => {
  it("announces itself", () => {
    render(<ErrorBanner>Something went wrong.</ErrorBanner>);
    expect(screen.getByRole("alert")).toHaveTextContent("Something went wrong.");
  });
});

describe("EmptyState", () => {
  it("renders its title, description and action", () => {
    render(
      <EmptyState
        title="No tokens yet"
        description="Create one to get started."
        action={<Button>Create</Button>}
      />,
    );
    expect(screen.getByText("No tokens yet")).toBeInTheDocument();
    expect(screen.getByText("Create one to get started.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create" })).toBeInTheDocument();
  });
});

describe("Chip", () => {
  it("says whether it is on", async () => {
    const onClick = vi.fn();
    render(
      <Chip pressed={false} onClick={onClick} data-plan-type-filter="Bug">
        Bug
      </Chip>,
    );
    const chip = screen.getByRole("button", { name: "Bug", pressed: false });
    await userEvent.click(chip);
    expect(onClick).toHaveBeenCalled();
    expect(chip).toHaveAttribute("data-plan-type-filter", "Bug");
  });
});

describe("OptionCard", () => {
  it("is a radio when it is one of a choice", () => {
    render(<OptionCard title="Scrum" description="Sprints" checked onSelect={() => {}} data-board-type="scrum" />);
    expect(screen.getByRole("radio", { name: /Scrum/, checked: true })).toHaveAttribute("data-board-type", "scrum");
  });

  it("is a plain button when it acts", async () => {
    const onSelect = vi.fn();
    render(<OptionCard title="Burndown" onSelect={onSelect} />);
    await userEvent.click(screen.getByRole("button", { name: "Burndown" }));
    expect(onSelect).toHaveBeenCalled();
    expect(screen.queryByRole("radio")).toBeNull();
  });
});

describe("SelectInput", () => {
  it("is named by its aria-label alone", () => {
    render(
      <SelectInput aria-label="Assignee" controlSize="sm">
        <option>Nobody</option>
      </SelectInput>,
    );
    expect(screen.getByRole("combobox", { name: "Assignee" })).toBeInTheDocument();
  });
});

describe("Button link", () => {
  it("is still a button", () => {
    render(<Button variant="link">Remove</Button>);
    expect(screen.getByRole("button", { name: "Remove" })).toBeInTheDocument();
  });
});
