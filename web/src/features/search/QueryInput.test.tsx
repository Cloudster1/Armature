import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ApiError } from "@/api/client";
import type { Suggestions } from "@/api/issues";

const navigate = vi.fn();
const state = { suggestions: undefined as Suggestions | undefined, asked: [] as unknown[] };

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("@/api/issues", () => ({
  useSuggestions: (query: unknown, enabled: boolean) => {
    if (enabled) state.asked.push(query);
    return { data: enabled ? state.suggestions : undefined };
  },
}));
vi.mock("@/api/filters", () => ({ useSavedFilters: () => ({ data: { filters: [{ id: "f1", name: "Mine", query: "assignee = currentUser()" }] } }) }));

const { QueryInput, applyCompletion } = await import("./QueryInput");

beforeEach(() => {
  state.suggestions = undefined;
  state.asked = [];
  navigate.mockReset();
  localStorage.clear();
});

describe("QueryInput", () => {
  it("applies on Enter with the text trimmed", () => {
    const onSubmit = vi.fn();
    render(<QueryInput value="" onSubmit={onSubmit} compact />);
    const input = screen.getByLabelText("Query");
    fireEvent.change(input, { target: { value: "  type = Bug " } });
    fireEvent.submit(input.closest("form")!);
    expect(onSubmit).toHaveBeenCalledWith("type = Bug");
  });

  it("puts a caret under the character the server named", () => {
    const error = new ApiError(400, { code: "bad_query", message: 'Unknown field "assigne". Did you mean "assignee"?', position: 1 });
    render(<QueryInput value="assigne = currentUser()" onSubmit={() => {}} error={error} compact />);
    expect(screen.getByRole("alert").textContent).toContain('Did you mean "assignee"?');
    expect(document.querySelector("[data-query-caret]")?.textContent).toBe("^");
  });

  it("does not show what is not a query error", () => {
    render(<QueryInput value="type = Bug" onSubmit={() => {}} error={new Error("network")} compact />);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // The help sits behind the info button, so the page is not a manual by default.
  it("offers the help behind its button, with an example per field", async () => {
    render(<QueryInput value="" onSubmit={() => {}} />);
    expect(screen.queryByText("statusCategory != done")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "What a query can say" }));
    expect(screen.getByRole("dialog", { name: "What a query can say" })).toBeTruthy();
    expect(screen.getByText("statusCategory != done")).toBeTruthy();
  });
});

describe("the list under the bar", () => {
  const typed = (): Suggestions => ({
    completion: { slot: "field", prefix: "sta", from: 0, to: 3, quoted: false, words: ["status"] },
    words: [{ text: "status", detail: "field" }, { text: "start", detail: "field" }],
    issues: [{ key: "ALP-7", summary: "Stale sessions" } as Suggestions["issues"][number]],
  });

  it("offers the words at the caret and the issues the text finds, and a picked word replaces the caret's", async () => {
    state.suggestions = typed();
    render(<QueryInput value="" onSubmit={() => {}} compact />);
    const input = screen.getByRole("combobox", { name: "Query" });
    await userEvent.click(input);
    await userEvent.type(input, "sta");
    const list = screen.getByRole("listbox", { name: "Suggestions" });
    expect(list.querySelectorAll('[data-suggestion="completion"]')).toHaveLength(2);
    expect(list.querySelector('[data-suggestion="issue"]')?.textContent).toContain("ALP-7");
    await userEvent.keyboard("{ArrowDown}{Enter}");
    expect((input as HTMLInputElement).value).toBe("status ");
  });

  it("opens an issue that is picked, and Enter with nothing picked runs the query", async () => {
    state.suggestions = typed();
    const onSubmit = vi.fn();
    render(<QueryInput value="" onSubmit={onSubmit} compact />);
    const input = screen.getByRole("combobox", { name: "Query" });
    await userEvent.click(input);
    await userEvent.type(input, "sta{Enter}");
    expect(onSubmit).toHaveBeenCalledWith("sta");
    await userEvent.type(input, "l");
    await userEvent.keyboard("{ArrowUp}{Enter}");
    expect(navigate).toHaveBeenCalledWith({ to: "/issues/$issueKey", params: { issueKey: "ALP-7" } });
  });

  // Focus alone opens nothing: a page that focuses the bar on arrival must
  // not be covered by the list.
  it("closes on Escape and offers what was searched before when the bar is empty", async () => {
    localStorage.setItem("armature.recentQueries", JSON.stringify(["type = Bug"]));
    const onSubmit = vi.fn();
    render(<QueryInput value="" onSubmit={onSubmit} />);
    const input = screen.getByRole("combobox", { name: "Query" });
    await userEvent.click(input);
    expect(screen.queryByRole("listbox")).toBeNull();
    await userEvent.keyboard("{ArrowDown}");
    const list = screen.getByRole("listbox", { name: "Suggestions" });
    expect(list.querySelector('[data-suggestion="recent"]')?.textContent).toContain("type = Bug");
    expect(list.querySelector('[data-suggestion="filter"]')?.textContent).toContain("Mine");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).toBeNull();
    await userEvent.keyboard("{ArrowDown}{ArrowDown}{Enter}");
    expect(onSubmit).toHaveBeenCalledWith("type = Bug");
  });
});

describe("applyCompletion", () => {
  it("replaces the word at the caret and closes a quote it opened", () => {
    expect(applyCompletion("status = do", { from: 9, to: 11, quoted: false }, "Done")).toEqual({ text: "status = Done ", caret: 14 });
    expect(applyCompletion('summary ~ "pri', { from: 10, to: 14, quoted: true }, "printer")).toEqual({ text: 'summary ~ "printer" ', caret: 20 });
    expect(applyCompletion('status = "To', { from: 9, to: 12, quoted: true }, '"To Do"')).toEqual({ text: 'status = "To Do" ', caret: 17 });
  });
});
