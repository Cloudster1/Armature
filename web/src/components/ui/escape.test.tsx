import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { Button, ConfirmDialog, Dialog, DockedPanel, Drawer, Menu } from "./index";

// One Escape is one step back: it closes the overlay on top and leaves the
// ones underneath, with focus back on what opened the one that closed.

function MoreMenu() {
  return (
    <Menu
      label="More"
      trigger={(props) => (
        <Button onClick={props.toggle} aria-haspopup={props["aria-haspopup"]} aria-expanded={props["aria-expanded"]}>
          More
        </Button>
      )}
      items={[{ label: "Clone", onSelect: () => {} }]}
    />
  );
}

describe("Escape with overlays stacked", () => {
  it("closes a menu inside a drawer and leaves the drawer open", async () => {
    function Host() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <Button onClick={() => setOpen(true)}>Open issue</Button>
          <Drawer open={open} onClose={() => setOpen(false)} title="ALP-12">
            <MoreMenu />
          </Drawer>
        </>
      );
    }
    render(<Host />);
    const opener = screen.getByRole("button", { name: "Open issue" });
    await userEvent.click(opener);
    const trigger = screen.getByRole("button", { name: "More" });
    await userEvent.click(trigger);
    expect(screen.getByRole("menu", { name: "More" })).toBeInTheDocument();

    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "ALP-12" })).toBeInTheDocument();
    expect(document.activeElement).toBe(trigger);

    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(document.activeElement).toBe(opener);
  });

  it("closes a confirm inside a dialog and leaves the dialog open", async () => {
    // The host re-renders as the confirm opens, so the dialog gets a new
    // onClose underneath it; that must not lift the dialog above the confirm.
    function Host() {
      const [open, setOpen] = useState(false);
      const [confirming, setConfirming] = useState(false);
      return (
        <>
          <Button onClick={() => setOpen(true)}>Edit team</Button>
          <Dialog open={open} onClose={() => setOpen(false)} title="Platform">
            <Button onClick={() => setConfirming(true)}>Remove member</Button>
          </Dialog>
          <ConfirmDialog open={confirming} onClose={() => setConfirming(false)} onConfirm={() => {}} noun="member" verb="Remove" body="Ada leaves the team." />
        </>
      );
    }
    render(<Host />);
    const opener = screen.getByRole("button", { name: "Edit team" });
    await userEvent.click(opener);
    const remove = screen.getByRole("button", { name: "Remove member" });
    await userEvent.click(remove);
    expect(screen.getByRole("dialog", { name: "Remove member?" })).toBeInTheDocument();

    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Remove member?" })).not.toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Platform" })).toBeInTheDocument();
    expect(document.activeElement).toBe(remove);

    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(document.activeElement).toBe(opener);
  });

  it("closes a menu inside a docked panel and leaves the panel open", async () => {
    const onClose = vi.fn();
    render(
      <DockedPanel open onClose={onClose} title="ALP-12" width={400}>
        <MoreMenu />
      </DockedPanel>,
    );
    await userEvent.click(screen.getByRole("button", { name: "More" }));
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("Drawer", () => {
  it("leaves Escape to a field that is being typed in, as the docked panel does", async () => {
    const onClose = vi.fn();
    render(
      <Drawer open onClose={onClose} title="ALP-12">
        <textarea aria-label="Comment" />
        <div aria-label="Rich comment" role="textbox" tabIndex={0} contentEditable suppressContentEditableWarning />
      </Drawer>,
    );
    const comment = screen.getByRole("textbox", { name: "Comment" });
    await userEvent.type(comment, "half a thought");
    await userEvent.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
    expect(comment).toHaveValue("half a thought");

    screen.getByRole("textbox", { name: "Rich comment" }).focus();
    await userEvent.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();

    screen.getByRole("button", { name: "Close" }).focus();
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
