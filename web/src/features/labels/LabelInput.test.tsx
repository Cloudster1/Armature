import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import type { LabelRef } from "@/api/issues";
import type { Label } from "@/api/labels";
import { LABEL_SUGGESTIONS } from "@/config";
import { LabelInput } from "./LabelInput";

// A label field draws nothing until something is typed, then offers the few
// labels the server finds for it and the typed word itself.

const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });

const label = (name: string): Label => ({ id: `l-${name}`, name, color: "teal", issueCount: 1, createdAt: "", updatedAt: "" });

const everyLabel = ["backend", "bug", "debug", "frontend", "urgent"].map(label);

// The server's answer: names starting with the text first, then those holding it.
function serveLabels() {
  const asked: URLSearchParams[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const params = new URL(url, "http://localhost").searchParams;
      asked.push(params);
      const q = (params.get("q") ?? "").toLowerCase();
      const holding = everyLabel.filter((l) => l.name.includes(q));
      const ordered = [...holding.filter((l) => l.name.startsWith(q)), ...holding.filter((l) => !l.name.startsWith(q))];
      return json({ labels: ordered.slice(0, Number(params.get("limit") ?? ordered.length)) });
    }),
  );
  return asked;
}

function Harness({ start = [], mode = "many", create = true, onChange }: { start?: string[]; mode?: "many" | "one"; create?: boolean; onChange?: (names: string[], labels: LabelRef[]) => void }) {
  const [value, setValue] = useState(start);
  return (
    <LabelInput
      aria-label="Add a label"
      value={value}
      mode={mode}
      create={create}
      onChange={(names, labels) => {
        setValue(names);
        onChange?.(names, labels);
      }}
    />
  );
}

function draw(props: Parameters<typeof Harness>[0] = {}) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Harness {...props} />
    </QueryClientProvider>,
  );
  return screen.getByRole("combobox", { name: "Add a label" });
}

const chosen = () => [...document.querySelectorAll("[data-label]")].map((el) => el.getAttribute("data-label"));
const offered = () => screen.queryAllByRole("option").map((el) => el.getAttribute("data-label-option"));

describe("LabelInput", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("shows no label before anything is typed", async () => {
    const asked = serveLabels();
    const box = draw();
    await userEvent.click(box);
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    expect(screen.queryAllByRole("option")).toHaveLength(0);
    expect(box).toHaveAttribute("aria-expanded", "false");
    expect(asked).toHaveLength(0);
  });

  it("asks for a few labels holding the text and keeps those starting with it first", async () => {
    const asked = serveLabels();
    const box = draw();
    await userEvent.type(box, "bu");
    await waitFor(() => expect(offered()).toEqual(["bug", "debug", "bu"]));
    expect(asked.at(-1)?.get("q")).toBe("bu");
    expect(asked.at(-1)?.get("limit")).toBe(String(LABEL_SUGGESTIONS));
    expect(screen.getByRole("option", { name: "Create bu" })).toBeInTheDocument();
    expect(box).toHaveAttribute("aria-expanded", "true");
  });

  // The word itself is already a label, so there is nothing to create.
  it("offers to create the typed word only when no label is called that", async () => {
    serveLabels();
    const box = draw();
    await userEvent.type(box, "BUG");
    await waitFor(() => expect(offered()).toEqual(["bug", "debug"]));
    expect(screen.queryByRole("option", { name: /^Create/ })).not.toBeInTheDocument();
  });

  it("adds the suggestion the arrow keys reach", async () => {
    serveLabels();
    const box = draw();
    await userEvent.type(box, "end");
    await waitFor(() => expect(offered()).toEqual(["backend", "frontend", "end"]));
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    expect(box).toHaveAttribute("aria-activedescendant", screen.getByRole("option", { name: "frontend" }).id);
    await userEvent.keyboard("{Enter}");
    expect(chosen()).toEqual(["frontend"]);
    expect(box).toHaveValue("");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("adds the typed word on Enter or a comma, and does not suggest what is already chosen", async () => {
    serveLabels();
    const box = draw({ start: ["backend"] });
    await userEvent.type(box, "fresh,");
    expect(chosen()).toEqual(["backend", "fresh"]);
    await userEvent.type(box, "back");
    await waitFor(() => expect(offered()).toEqual(["back"]));
    await userEvent.keyboard("{Enter}");
    expect(chosen()).toEqual(["backend", "fresh", "back"]);
  });

  it("removes the last label with Backspace on an empty box, or one by its button", async () => {
    serveLabels();
    const box = draw({ start: ["backend", "urgent", "bug"] });
    await userEvent.type(box, "{Backspace}");
    expect(chosen()).toEqual(["backend", "urgent"]);
    await userEvent.click(screen.getByRole("button", { name: "Remove label backend" }));
    expect(chosen()).toEqual(["urgent"]);
  });

  it("closes the suggestions on Escape and leaves the typing alone", async () => {
    serveLabels();
    const box = draw();
    await userEvent.type(box, "bu");
    await waitFor(() => expect(screen.getByRole("listbox")).toBeInTheDocument());
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    expect(box).toHaveValue("bu");
  });

  it("holds one label in one mode, the newest replacing the last", async () => {
    serveLabels();
    const onChange = vi.fn();
    const box = draw({ start: ["backend"], mode: "one", onChange });
    await userEvent.type(box, "urg");
    await waitFor(() => expect(offered()).toContain("urgent"));
    await userEvent.click(screen.getByRole("option", { name: "urgent" }));
    expect(chosen()).toEqual(["urgent"]);
    expect(onChange).toHaveBeenLastCalledWith(["urgent"], [expect.objectContaining({ id: "l-urgent", name: "urgent" })]);
  });

  // A filter can only narrow by a label that exists.
  it("offers no new word when it only picks existing labels", async () => {
    serveLabels();
    const box = draw({ create: false });
    await userEvent.type(box, "nothing");
    await waitFor(() => expect(screen.queryByRole("option", { name: /^Create/ })).not.toBeInTheDocument());
    await userEvent.keyboard("{Enter}");
    expect(chosen()).toEqual([]);
  });
});
