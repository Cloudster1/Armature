import { describe, expect, it, vi } from "vitest";
import { render, within } from "@testing-library/react";
import type { Group } from "@/api/access";

const groups: Group[] = [
  { id: "g-everyone", name: "Everyone", source: "local", builtin: "everyone", memberCount: 1200, roleCount: 2 },
  { id: "g-release", name: "Release managers", source: "local", memberCount: 1, roleCount: 1 },
];
const fetched: string[] = [];

vi.mock("@/api/access", async () => {
  const actual = await vi.importActual<typeof import("@/api/access")>("@/api/access");
  const idle = { mutate: vi.fn(), isPending: false, error: null };
  return {
    groupLabel: actual.groupLabel,
    useGroups: () => ({ data: { groups }, isLoading: false, error: null }),
    useGroup: (id: string) => {
      fetched.push(id);
      return { data: { group: { ...groups[1], members: [{ userId: "u-ada", name: "Ada Lovelace" }] } } };
    },
    useAddToGroup: () => idle,
    useRemoveFromGroup: () => idle,
    useDeleteGroup: () => idle,
    useCreateGroup: () => idle,
  };
});
vi.mock("@/features/people/PersonPicker", () => ({ PersonPicker: () => null }));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => async () => true }));

const { GroupList } = await import("./GroupList");

describe("GroupList", () => {
  // Everyone holds every member, so it is read, never edited, and its thousands are not listed.
  it("shows Everyone first, built in, with nothing to edit and no list of members", () => {
    const { container } = render(<GroupList />);
    const cards = [...container.querySelectorAll("[data-group]")];
    expect(cards.map((card) => card.getAttribute("data-group"))).toEqual(["Everyone", "Release managers"]);
    const everyone = within(cards[0] as HTMLElement);
    expect(everyone.getByText("built in")).toBeTruthy();
    expect(everyone.getByText("1200 people · 2 roles")).toBeTruthy();
    expect(everyone.queryByRole("button")).toBeNull();
    expect(fetched).not.toContain("g-everyone");
    expect(within(cards[1] as HTMLElement).getByRole("button", { name: "Delete" })).toBeTruthy();
  });
});
