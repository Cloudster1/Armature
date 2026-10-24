import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import type { Member } from "@/api/people";

const all = [
  { id: "ada", name: "Ada Lovelace", email: "ada@example.com", role: "member" },
  { id: "cus", name: "Carla Customer", email: "carla@example.com", role: "customer" },
  { id: "grace", name: "Grace Hopper", email: "grace@example.com", role: "admin" },
] as Member[];
const state = { searched: [] as Array<{ q: string; staff: boolean }> };

// The server's search, over name and address; the picker filters nothing itself.
vi.mock("@/api/people", () => ({
  useMemberSearch: (q: string, { staff = false }: { staff?: boolean } = {}) => {
    const last = state.searched.at(-1);
    if (last?.q !== q || last.staff !== staff) state.searched.push({ q, staff });
    const lower = q.toLowerCase();
    const members = all.filter((m) => (!staff || m.role !== "customer") && (!lower || m.name.toLowerCase().includes(lower) || m.email.includes(lower)));
    return { data: { members, total: members.length, limit: 20, offset: 0 } };
  },
}));

const { PersonPicker } = await import("./PersonPicker");

function Harness({
  initial = "",
  selected,
  exclude,
  staff,
}: {
  initial?: string;
  selected?: { id: string; name: string };
  exclude?: string[];
  staff?: boolean;
}) {
  const [value, setValue] = useState(initial);
  return (
    <form onSubmit={() => setValue("submitted")}>
      <PersonPicker label="Assignee" none="Nobody" value={value} onChange={setValue} selected={selected} exclude={exclude} staff={staff} />
      <output data-testid="chosen">{value}</output>
    </form>
  );
}

function offered(): string[] {
  return [...(screen.getByLabelText("Assignee") as HTMLSelectElement).options].map((option) => option.value);
}

beforeEach(() => {
  state.searched = [];
});

describe("PersonPicker", () => {
  it("asks the server for what is typed and offers who it found", async () => {
    render(<Harness />);
    expect(offered()).toEqual(["", "ada", "cus", "grace"]);
    await userEvent.type(screen.getByLabelText("Find a person for Assignee"), "grace@");
    await waitFor(() => expect(offered()).toEqual(["", "grace"]));
    expect(state.searched.map((each) => each.q)).toEqual(["", "grace@"]);
  });

  it("picks somebody, and keeps them chosen when the search moves past them", async () => {
    render(<Harness />);
    await userEvent.selectOptions(screen.getByLabelText("Assignee"), "grace");
    expect(screen.getByTestId("chosen")).toHaveTextContent("grace");
    await userEvent.type(screen.getByLabelText("Find a person for Assignee"), "ada");
    await waitFor(() => expect(offered()).toEqual(["", "grace", "ada"]));
    expect(screen.getByLabelText("Assignee")).toHaveValue("grace");
  });

  it("shows who the record names without finding them first", async () => {
    render(<Harness initial="zoe" selected={{ id: "zoe", name: "Zoe Zimmer" }} />);
    expect(offered()).toEqual(["", "zoe", "ada", "cus", "grace"]);
    expect(screen.getByRole("option", { name: "Zoe Zimmer" })).toHaveProperty("selected", true);
  });

  it("asks for the staff alone when told to, and leaves out who it is told to", () => {
    render(<Harness staff exclude={["ada"]} />);
    expect(state.searched).toEqual([{ q: "", staff: true }]);
    expect(offered()).toEqual(["", "grace"]);
  });

  it("says when nobody matches, and Enter in the search does not submit the form", async () => {
    render(<Harness />);
    await userEvent.type(screen.getByLabelText("Find a person for Assignee"), "zzz{Enter}");
    await waitFor(() => expect(screen.getByText("Nobody here matches that. Try part of their name or their address.")).toBeInTheDocument());
    expect(offered()).toEqual([""]);
    expect(screen.getByTestId("chosen")).toHaveTextContent("");
  });
});
