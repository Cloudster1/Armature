import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Checkbox, Field, IconButton, Segmented, Select, Switch, Tabs } from "./index";
import { Icon } from "../icons";

describe("Field", () => {
  it("keeps the id the browser tests read, and can be a textarea", () => {
    render(<Field label="Work email" />);
    expect(screen.getByLabelText("Work email")).toHaveAttribute("id", "field-work-email");
    render(<Field label="Details" rows={3} hint="Anything that helps." />);
    const details = screen.getByLabelText("Details");
    expect(details.tagName).toBe("TEXTAREA");
    expect(details).toHaveAccessibleDescription("Anything that helps.");
  });
});

describe("Select", () => {
  it("derives its id from the label and describes itself by its error", () => {
    render(
      <Select label="Issue type" error="Pick one.">
        <option>Bug</option>
      </Select>,
    );
    const select = screen.getByLabelText("Issue type");
    expect(select).toHaveAttribute("id", "field-issue-type");
    expect(select).toHaveAttribute("aria-invalid", "true");
    expect(select).toHaveAccessibleDescription("Pick one.");
  });
});

describe("IconButton", () => {
  it("is named by its label", () => {
    render(<IconButton icon={<Icon.Trash />} label="Delete team" />);
    expect(screen.getByRole("button", { name: "Delete team" })).toBeInTheDocument();
  });
});

describe("Switch", () => {
  it("is a switch that toggles on click and on Space", async () => {
    const onChange = vi.fn();
    render(<Switch label="Hide done" checked={false} onChange={onChange} />);
    const sw = screen.getByRole("switch", { name: "Hide done" });
    expect(sw).toHaveAttribute("aria-checked", "false");
    await userEvent.click(sw);
    expect(onChange).toHaveBeenCalledWith(true);
    sw.focus();
    await userEvent.keyboard(" ");
    expect(onChange).toHaveBeenCalledTimes(2);
  });
});

describe("Checkbox", () => {
  it("is one click target with its words", async () => {
    const onChange = vi.fn();
    render(<Checkbox label="Internal note" onChange={onChange} />);
    await userEvent.click(screen.getByText("Internal note"));
    expect(onChange).toHaveBeenCalled();
  });
});

describe("Segmented", () => {
  it("moves the choice with the arrow keys", async () => {
    const onChange = vi.fn();
    render(<Segmented label="Zoom" value="weeks" options={[{ value: "weeks", label: "Weeks" }, { value: "months", label: "Months" }]} onChange={onChange} />);
    const weeks = screen.getByRole("button", { name: "Weeks" });
    expect(weeks).toHaveAttribute("aria-pressed", "true");
    weeks.focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(onChange).toHaveBeenCalledWith("months");
  });
});

describe("Tabs", () => {
  it("selects with a click and with the arrow keys", async () => {
    const onChange = vi.fn();
    render(<Tabs label="Activity" value="all" tabs={[{ value: "all", label: "All" }, { value: "comments", label: "Comments" }]} onChange={onChange} />);
    expect(screen.getByRole("tab", { name: "All" })).toHaveAttribute("aria-selected", "true");
    await userEvent.click(screen.getByRole("tab", { name: "Comments" }));
    expect(onChange).toHaveBeenCalledWith("comments");
    screen.getByRole("tab", { name: "All" }).focus();
    await userEvent.keyboard("{End}");
    expect(onChange).toHaveBeenLastCalledWith("comments");
  });
});
