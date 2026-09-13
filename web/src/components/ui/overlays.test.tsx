import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { Avatar, Button, ConfirmDialog, Dialog, DockedPanel, Drawer, Menu, Popover, Skeleton, ToastProvider, Tooltip, useToast } from "./index";
import { SKELETON_DELAY_MS, TOOLTIP_DELAY_MS } from "@/config";

describe("Dialog", () => {
  it("is modal, labelled by its title, closes on Escape and returns focus", async () => {
    function Host() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <Button onClick={() => setOpen(true)}>Open</Button>
          <Dialog open={open} onClose={() => setOpen(false)} title="Rename project" description="A new name.">
            <input aria-label="Name" />
          </Dialog>
        </>
      );
    }
    render(<Host />);
    const opener = screen.getByRole("button", { name: "Open" });
    await userEvent.click(opener);
    const dialog = screen.getByRole("dialog", { name: "Rename project" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAccessibleDescription("A new name.");
    expect(document.activeElement).toBe(screen.getByLabelText("Name"));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(document.activeElement).toBe(opener);
  });

  it("keeps Tab inside", async () => {
    render(
      <Dialog open onClose={() => {}} title="Two buttons" footer={<><Button>One</Button><Button>Two</Button></>} />,
    );
    screen.getByRole("button", { name: "Two" }).focus();
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" }));
  });
});

describe("ConfirmDialog", () => {
  it("names the verb and the noun on the button that does it", async () => {
    const onConfirm = vi.fn();
    render(<ConfirmDialog open onClose={() => {}} onConfirm={onConfirm} noun="team" body="Platform has 3 members." />);
    expect(screen.getByRole("dialog", { name: "Delete team?" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Delete team" }));
    expect(onConfirm).toHaveBeenCalled();
  });
});

describe("Drawer", () => {
  it("opens from the side with its title and closes on Escape", async () => {
    const onClose = vi.fn();
    render(
      <Drawer open onClose={onClose} title="ALP-12">
        <p>Body</p>
      </Drawer>,
    );
    expect(screen.getByRole("dialog", { name: "ALP-12" })).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });
});

describe("DockedPanel", () => {
  it("sits beside the page without a scrim or a trap, and Escape closes it", async () => {
    const onClose = vi.fn();
    render(
      <>
        <button>Outside</button>
        <DockedPanel open onClose={onClose} title="ALP-12" width={400}>
          <button>Inside</button>
        </DockedPanel>
      </>,
    );
    const panel = screen.getByRole("complementary", { name: "ALP-12" });
    expect(panel).not.toHaveAttribute("aria-modal");
    expect(screen.queryByRole("dialog")).toBeNull();
    // Focus went in on open, and Tab is free to leave again.
    expect(screen.getByRole("button", { name: "Inside" })).toHaveFocus();
    await userEvent.tab();
    await userEvent.tab();
    expect(screen.getByRole("button", { name: "Outside" })).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("leaves Escape to a field that is being typed in", async () => {
    const onClose = vi.fn();
    render(
      <DockedPanel open onClose={onClose} title="ALP-12">
        <textarea aria-label="Comment" />
      </DockedPanel>,
    );
    await userEvent.click(screen.getByRole("textbox", { name: "Comment" }));
    await userEvent.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("Menu", () => {
  it("opens on the trigger, moves with arrows, selects, and returns focus", async () => {
    const archive = vi.fn();
    render(
      <Menu
        label="Project actions"
        trigger={(props) => (
          <Button variant="ghost" onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]}>
            More
          </Button>
        )}
        items={[
          { label: "Settings", onSelect: () => {} },
          { label: "Archive", onSelect: archive, danger: true },
        ]}
      />,
    );
    const trigger = screen.getByRole("button", { name: "More" });
    await userEvent.click(trigger);
    expect(screen.getByRole("menu", { name: "Project actions" })).toBeInTheDocument();
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Settings" }));
    await userEvent.keyboard("{ArrowDown}");
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Archive" }));
    await userEvent.keyboard("{Enter}");
    expect(archive).toHaveBeenCalled();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(document.activeElement).toBe(trigger);
  });

  it("closes on Escape without selecting", async () => {
    const pick = vi.fn();
    render(<Menu label="m" trigger={(p) => <button onClick={p.toggle}>Open</button>} items={[{ label: "Only", onSelect: pick }]} />);
    await userEvent.click(screen.getByRole("button", { name: "Open" }));
    await userEvent.keyboard("{Escape}");
    expect(pick).not.toHaveBeenCalled();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });
});

describe("Popover", () => {
  it("closes on a press outside and on Escape", async () => {
    function Host() {
      const [open, setOpen] = useState(true);
      return (
        <>
          <p>Outside</p>
          <Popover open={open} onClose={() => setOpen(false)} label="Filters" trigger={<button onClick={() => setOpen(true)}>Filters</button>}>
            <input aria-label="Label" />
          </Popover>
        </>
      );
    }
    render(<Host />);
    expect(screen.getByRole("dialog", { name: "Filters" })).toBeInTheDocument();
    fireEvent.pointerDown(screen.getByText("Outside"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Filters" }));
    expect(screen.getByRole("dialog", { name: "Filters" })).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});

describe("Tooltip", () => {
  it("shows after the delay on hover and describes its child", () => {
    vi.useFakeTimers();
    render(
      <Tooltip text="Collapse the sidebar">
        <button>Collapse</button>
      </Tooltip>,
    );
    fireEvent.pointerEnter(screen.getByRole("button", { name: "Collapse" }).parentElement!);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(TOOLTIP_DELAY_MS);
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent("Collapse the sidebar");
    vi.useRealTimers();
  });
});

describe("Toast", () => {
  it("announces a success politely and an error assertively, with an action", async () => {
    const undo = vi.fn();
    function Host() {
      const toast = useToast();
      return (
        <>
          <Button onClick={() => toast.success("Created ALP-13", { action: { label: "Undo", onClick: undo } })}>Make</Button>
          <Button onClick={() => toast.error("That failed.")}>Fail</Button>
        </>
      );
    }
    render(
      <ToastProvider>
        <Host />
      </ToastProvider>,
    );
    await userEvent.click(screen.getByRole("button", { name: "Make" }));
    expect(screen.getByRole("status")).toHaveTextContent("Created ALP-13");
    await userEvent.click(screen.getByRole("button", { name: "Fail" }));
    expect(screen.getByRole("alert")).toHaveTextContent("That failed.");
    await userEvent.click(screen.getByRole("button", { name: "Undo" }));
    expect(undo).toHaveBeenCalled();
    expect(screen.queryByText("Created ALP-13")).not.toBeInTheDocument();
  });
});

describe("Skeleton", () => {
  it("shows nothing before the delay and grey after", () => {
    vi.useFakeTimers();
    const { container } = render(<Skeleton lines={2} />);
    expect(container.querySelector("[data-skeleton='pending']")).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(SKELETON_DELAY_MS);
    });
    expect(container.querySelector("[data-skeleton]:not([data-skeleton='pending'])")).toBeInTheDocument();
    vi.useRealTimers();
  });
});

describe("Avatar", () => {
  it("shows the picture when there is one and the initials when it breaks", () => {
    render(<Avatar name="Ada Lovelace" src="/api/v1/users/x/avatar" />);
    const img = screen.getByRole("img", { name: "Ada Lovelace" });
    fireEvent.error(img);
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
    expect(screen.getByText("AL")).toBeInTheDocument();
  });
});
