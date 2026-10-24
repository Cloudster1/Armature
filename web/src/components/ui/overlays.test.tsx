import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { Avatar, Button, ConfirmDialog, Dialog, DockedPanel, Drawer, Menu, Popover, Skeleton, ToastProvider, Tooltip, useToast } from "./index";
import { MENU_GAP_PX, POPOVER_EDGE_PX, POPOVER_MAX_HEIGHT_PX, SKELETON_DELAY_MS, TOOLTIP_DELAY_MS } from "@/config";
import { anchoredBox } from "./overlay";

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
      <Dialog
        open
        onClose={() => {}}
        title="Two buttons"
        footer={
          <>
            <Button>One</Button>
            <Button>Two</Button>
          </>
        }
      />,
    );
    screen.getByRole("button", { name: "Two" }).focus();
    await userEvent.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" }));
  });
});

// jsdom lays nothing out, so what keeps a tall dialog in reach is read off the
// structure; the browser suite turns the wheel over it.
describe("Dialog taller than the window", () => {
  it("sits on a layer that scrolls like a page, with its title and buttons pinned to its edges", () => {
    render(
      <Dialog open onClose={() => {}} title="Many fields" footer={<Button>Create issue</Button>}>
        <input aria-label="Last field" />
      </Dialog>,
    );
    const dialog = screen.getByRole("dialog", { name: "Many fields" });
    const layer = dialog.parentElement!;
    expect(layer).toHaveAttribute("data-dialog-backdrop");
    expect(layer).toHaveClass("fixed", "inset-0", "overflow-y-auto", "overscroll-contain");
    expect(dialog.style.maxHeight).toBe("");
    expect(dialog.querySelector("[data-dialog-body]")).not.toHaveClass("overflow-y-auto");
    expect(dialog.querySelector("[data-dialog-header]")).toHaveClass("tall:sticky", "tall:top-0");
    expect(dialog.querySelector("[data-dialog-footer]")).toHaveClass("tall:sticky", "tall:bottom-0");
    expect(dialog.querySelector("[data-dialog-footer]")).toContainElement(screen.getByRole("button", { name: "Create issue" }));
  });

  it("stays open when its scrollbar is pressed and closes on a press beside it", () => {
    const onClose = vi.fn();
    render(<Dialog open onClose={onClose} title="Many fields" />);
    const layer = screen.getByRole("dialog").parentElement!;
    Object.defineProperty(layer, "clientWidth", { value: 1265 });
    // jsdom's pointer events carry no coordinates; a mouse event of that type does.
    const press = (clientX: number) => fireEvent(layer, new MouseEvent("pointerdown", { bubbles: true, clientX }));
    press(1272);
    expect(onClose).not.toHaveBeenCalled();
    press(20);
    expect(onClose).toHaveBeenCalledOnce();
  });
});

describe("anchoredBox", () => {
  const view = { width: 1280, height: 600 };
  const anchor = (top: number, bottom = top + 30) => ({ top, bottom, left: 100, right: 300, height: bottom - top });

  it("goes below with the room that is left there when it fits", () => {
    const box = anchoredBox(anchor(100), { width: 200, height: 300 }, view);
    expect(box.top).toBe(130 + MENU_GAP_PX);
    expect(box.left).toBe(100);
    expect(box.maxHeight).toBe(Math.min(POPOVER_MAX_HEIGHT_PX, 600 - POPOVER_EDGE_PX - 130 - MENU_GAP_PX));
  });

  it("goes above when only that side has room", () => {
    const box = anchoredBox(anchor(500), { width: 200, height: 300 }, view);
    expect(box.top).toBe(500 - MENU_GAP_PX - 300);
    expect(box.maxHeight).toBe(500 - MENU_GAP_PX - POPOVER_EDGE_PX);
  });

  // The project switcher on a laptop: twenty projects below a trigger high on the page.
  it("takes the roomier side and scrolls when neither side has room", () => {
    const below = anchoredBox(anchor(200), { width: 200, height: 900 }, view);
    expect(below.top).toBe(230 + MENU_GAP_PX);
    expect(below.maxHeight).toBe(600 - POPOVER_EDGE_PX - 230 - MENU_GAP_PX);
    const above = anchoredBox(anchor(400), { width: 200, height: 900 }, view);
    expect(above.maxHeight).toBe(400 - MENU_GAP_PX - POPOVER_EDGE_PX);
    expect(above.top).toBe(POPOVER_EDGE_PX);
  });

  it("never grows past the cap on a tall window", () => {
    const box = anchoredBox(anchor(10), { width: 200, height: 5000 }, { width: 1280, height: 4000 });
    expect(box.maxHeight).toBe(POPOVER_MAX_HEIGHT_PX);
  });

  it("opens upward when asked and there is room", () => {
    const box = anchoredBox(anchor(400), { width: 200, height: 100 }, view, { side: "top" });
    expect(box.top).toBe(400 - MENU_GAP_PX - 100);
  });

  it("keeps a panel beside its anchor inside the window", () => {
    const box = anchoredBox(anchor(580, 590), { width: 200, height: 300 }, view, { side: "right" });
    expect(box.left).toBe(300 + MENU_GAP_PX);
    expect(box.top).toBe(600 - POPOVER_EDGE_PX - 300);
  });

  it("lines up the right edges for an end-aligned panel", () => {
    expect(anchoredBox(anchor(100), { width: 200, height: 100 }, view, { align: "end" }).right).toBe(1280 - 300);
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
        <button type="button">Outside</button>
        <DockedPanel open onClose={onClose} title="ALP-12" width={400}>
          <button type="button">Inside</button>
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
    render(
      <Menu
        label="m"
        trigger={(p) => (
          <button type="button" onClick={p.toggle}>
            Open
          </button>
        )}
        items={[{ label: "Only", onSelect: pick }]}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Open" }));
    await userEvent.keyboard("{Escape}");
    expect(pick).not.toHaveBeenCalled();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("keeps a header's box for typing, and leaves it for the items by the arrows", async () => {
    const pick = vi.fn();
    function Host() {
      const [text, setText] = useState("");
      return (
        <Menu
          label="Switch"
          header={<input aria-label="Filter" value={text} onChange={(e) => setText(e.target.value)} />}
          trigger={(p) => (
            <button type="button" onClick={p.toggle}>
              Open
            </button>
          )}
          items={[{ label: "Apollo", onSelect: pick }]}
        />
      );
    }
    render(<Host />);
    await userEvent.click(screen.getByRole("button", { name: "Open" }));
    const box = screen.getByLabelText("Filter");
    expect(document.activeElement).toBe(box);
    await userEvent.keyboard("a b");
    expect(box).toHaveValue("a b");
    expect(document.activeElement).toBe(box);
    expect(pick).not.toHaveBeenCalled();
    await userEvent.keyboard("{ArrowDown}");
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Apollo" }));
  });

  it("draws a link as a plain link under its section's heading, and moves to it with the arrows", async () => {
    render(
      <Menu
        label="Your account"
        trigger={(p) => (
          <button type="button" onClick={p.toggle}>
            Open
          </button>
        )}
        items={[
          { label: "Your profile", onSelect: () => {} },
          { label: "Stator", href: "https://stator.example.test", section: "Switch to" },
        ]}
      />,
    );
    await userEvent.click(screen.getByRole("button", { name: "Open" }));
    const link = screen.getByRole("menuitem", { name: "Stator" });
    expect(link.tagName).toBe("A");
    expect(link).toHaveAttribute("href", "https://stator.example.test");
    expect(screen.getByText("Switch to")).toHaveAttribute("data-menu-section", "Switch to");
    expect(screen.getAllByRole("menuitem")).toHaveLength(2);
    await userEvent.keyboard("{ArrowDown}");
    expect(document.activeElement).toBe(link);
  });
});

describe("Popover", () => {
  it("closes on a press outside and on Escape", async () => {
    function Host() {
      const [open, setOpen] = useState(true);
      return (
        <>
          <p>Outside</p>
          <Popover
            open={open}
            onClose={() => setOpen(false)}
            label="Filters"
            trigger={
              <button type="button" onClick={() => setOpen(true)}>
                Filters
              </button>
            }
          >
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
        <button type="button">Collapse</button>
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
