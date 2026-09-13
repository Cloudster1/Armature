import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Doc } from "@/api/issues";
import { Editor, type EditorHandle } from "./Editor";

const written: Doc = { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "hello" }] }] };

beforeEach(() => localStorage.clear());

describe("Editor", () => {
  it("edits the document in place and reports every change as a document, or null when empty", async () => {
    const onChange = vi.fn();
    render(<Editor id="issue-description" value={written} onChange={onChange} />);
    const box = document.getElementById("issue-description")!;
    expect(box.getAttribute("contenteditable")).toBe("true");
    expect(box.textContent).toBe("hello");
    // Select the word the way a person would, then press Bold.
    box.focus();
    const range = document.createRange();
    range.selectNodeContents(box.querySelector("p")!);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
    await userEvent.click(screen.getByRole("button", { name: "Bold" }));
    const last = onChange.mock.calls.at(-1)?.[0] as Doc;
    expect(JSON.stringify(last)).toContain('"bold"');
    expect(screen.getByRole("button", { name: "Bold" }).getAttribute("aria-pressed")).toBe("true");
    await userEvent.type(box, " there");
    expect(JSON.stringify(onChange.mock.calls.at(-1)?.[0])).toContain("there");
  });

  it("shows the markdown of the document in the other mode, and reads it back", async () => {
    const onChange = vi.fn();
    render(<Editor id="issue-description" value={written} onChange={onChange} />);
    await userEvent.click(screen.getByRole("button", { name: "Markdown" }));
    const area = document.getElementById("issue-description") as HTMLTextAreaElement;
    expect(area.tagName).toBe("TEXTAREA");
    expect(area.value).toBe("hello");
    fireEvent.change(area, { target: { value: "## Plan\n\n- first" } });
    expect(onChange).toHaveBeenLastCalledWith({
      type: "doc",
      content: [
        { type: "heading", attrs: { level: 2 }, content: [{ type: "text", text: "Plan" }] },
        { type: "bulletList", content: [{ type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "first" }] }] }] },
      ],
    });
    expect(localStorage.getItem("armature.editor")).toBe("markdown");
    await userEvent.click(screen.getByRole("button", { name: "Rich" }));
    expect(document.getElementById("issue-description")?.querySelector("h2")?.textContent).toBe("Plan");
  });

  it("takes words from outside and can be emptied", () => {
    let handle: EditorHandle | undefined;
    const onChange = vi.fn();
    render(<Editor id="new-comment" value={null} onChange={onChange} handle={(h) => (handle = h)} />);
    act(() => handle!.insertMarkdown("Thanks, **done**."));
    expect(document.getElementById("new-comment")?.querySelector("strong")?.textContent).toBe("done");
    act(() => handle!.clear());
    expect(onChange).toHaveBeenLastCalledWith(null);
  });
});
