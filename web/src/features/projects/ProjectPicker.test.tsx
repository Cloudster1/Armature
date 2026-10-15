import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import type { Project } from "@/api/projects";

const all = [
  { key: "APO", name: "Apollo" },
  { key: "BOR", name: "Borealis" },
  { key: "CYG", name: "Cygnus" },
] as Project[];
const state = { searched: [] as string[] };

// The server's search, over key and name; the picker filters nothing itself.
vi.mock("@/api/projects", () => ({
  useProjectSearch: (q: string) => {
    if (state.searched.at(-1) !== q) state.searched.push(q);
    const lower = q.toLowerCase();
    return { data: { projects: all.filter((p) => !lower || p.key.toLowerCase().includes(lower) || p.name.toLowerCase().includes(lower)) } };
  },
  useProject: (key: string) => ({ data: key ? { project: all.find((p) => p.key === key) } : undefined }),
}));

const { ProjectPicker } = await import("./ProjectPicker");

function Harness({ initial = "", exclude }: { initial?: string; exclude?: string[] }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <ProjectPicker label="Project" value={value} onChange={setValue} exclude={exclude} />
      <output data-testid="chosen">{value}</output>
    </>
  );
}

function offered(): string[] {
  return [...(screen.getByLabelText("Project") as HTMLSelectElement).options].map((option) => option.value);
}

beforeEach(() => {
  state.searched = [];
});

describe("ProjectPicker", () => {
  it("asks the server for what is typed and offers what it found", async () => {
    render(<Harness />);
    expect(offered()).toEqual(["", "APO", "BOR", "CYG"]);
    await userEvent.type(screen.getByLabelText("Find a project for Project"), "bor");
    await waitFor(() => expect(offered()).toEqual(["", "BOR"]));
    expect(state.searched).toEqual(["", "bor"]);
  });

  it("picks a project, and keeps it chosen when the search moves past it", async () => {
    render(<Harness />);
    await userEvent.selectOptions(screen.getByLabelText("Project"), "CYG");
    expect(screen.getByTestId("chosen")).toHaveTextContent("CYG");
    await userEvent.type(screen.getByLabelText("Find a project for Project"), "apo");
    await waitFor(() => expect(offered()).toEqual(["", "CYG", "APO"]));
    expect(screen.getByLabelText("Project")).toHaveValue("CYG");
  });

  it("says when nothing matches, and how to find one", async () => {
    render(<Harness />);
    await userEvent.type(screen.getByLabelText("Find a project for Project"), "zzz");
    await waitFor(() => expect(screen.getByText("No project matches that. Try part of its key or its name.")).toBeInTheDocument());
    expect(offered()).toEqual([""]);
  });

  it("leaves out what it is told to", () => {
    render(<Harness exclude={["APO"]} />);
    expect(offered()).toEqual(["", "BOR", "CYG"]);
  });
});
