import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState, type ReactNode } from "react";
import { BOARD_SEARCH_DELAY_MS } from "@/config";

// The link is only an address here; following it is the router's job.
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, search, title }: { children: ReactNode; to: string; search: { q: string }; title?: string }) => (
    <a href={`${to}?q=${encodeURIComponent(search.q)}`} title={title}>
      {children}
    </a>
  ),
}));

const { QuickFilters } = await import("./QuickFilters");
import { EMPTY_QUICK_FILTER, type QuickFilter, type QuickFilterOptions } from "./quickFilter";

const options: QuickFilterOptions = {
  assignees: [{ id: "u1", name: "Ada" }],
  labels: [],
  types: [{ id: "t1", name: "Bug", icon: "bug", level: 0, isSubtask: false }],
  epics: [],
};

function Harness({
  initial,
  hidden,
  leftOut,
  onChange,
}: {
  initial: QuickFilter;
  hidden: number;
  leftOut?: { count: number; query: string; days: number };
  onChange: (filter: QuickFilter) => void;
}) {
  const [filter, setFilter] = useState(initial);
  return (
    <QuickFilters
      filter={filter}
      options={options}
      hidden={hidden}
      leftOut={leftOut}
      onChange={(next) => {
        onChange(next);
        setFilter(next);
      }}
    />
  );
}

describe("the quick filters", () => {
  it("say nothing is hidden and offer no Clear while nothing is chosen", () => {
    render(<Harness initial={EMPTY_QUICK_FILTER} hidden={0} onChange={() => {}} />);
    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();
    expect(screen.queryByText(/hidden by the filters/)).toBeNull();
  });

  it("say how many cards are hidden, and Clear empties the filter and the box", async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ ...EMPTY_QUICK_FILTER, mine: true, q: "pay" }} hidden={3} onChange={onChange} />);
    expect(screen.getByRole("status")).toHaveTextContent("3 cards hidden by the filters");
    expect(screen.getByRole("searchbox", { name: "Filter cards by key or summary" })).toHaveValue("pay");

    await userEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(onChange).toHaveBeenLastCalledWith(EMPTY_QUICK_FILTER);
    expect(screen.getByRole("searchbox")).toHaveValue("");
    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();
  });

  it("say how much finished work the board left out and link to it, filter or not", async () => {
    const query = "project = CP AND statusCategory = done AND (resolved < -14d OR (resolved IS EMPTY AND updated < -14d))";
    render(<Harness initial={EMPTY_QUICK_FILTER} hidden={0} leftOut={{ count: 3, query, days: 14 }} onChange={() => {}} />);
    const link = screen.getByRole("link", { name: "3 finished issues left out" });
    expect(link).toHaveAttribute("href", `/search?q=${encodeURIComponent(query)}`);
    expect(link).toHaveAttribute("title", "Finished more than 14 days ago. Open them in search.");
    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Only mine" }));
    expect(screen.getByRole("link", { name: "3 finished issues left out" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Clear filters" })).toBeInTheDocument();
  });

  it("say nothing of finished work a board keeps", () => {
    render(<Harness initial={{ ...EMPTY_QUICK_FILTER, mine: true }} hidden={1} onChange={() => {}} />);
    expect(screen.queryByText(/left out/)).toBeNull();
  });

  it("turns Only mine on and off", async () => {
    const onChange = vi.fn();
    render(<Harness initial={EMPTY_QUICK_FILTER} hidden={0} onChange={onChange} />);
    const mine = screen.getByRole("button", { name: "Only mine" });
    await userEvent.click(mine);
    expect(onChange).toHaveBeenLastCalledWith({ ...EMPTY_QUICK_FILTER, mine: true });
    expect(mine).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(mine);
    expect(onChange).toHaveBeenLastCalledWith({ ...EMPTY_QUICK_FILTER, mine: undefined });
  });

  it("offers a kind's values beside its chip, and offers no kind the board has none of", async () => {
    const onChange = vi.fn();
    render(<Harness initial={EMPTY_QUICK_FILTER} hidden={0} onChange={onChange} />);
    expect(screen.queryByRole("button", { name: /Label/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Epic/ })).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Type" }));
    await userEvent.click(screen.getByRole("button", { name: /Bug/ }));
    expect(onChange).toHaveBeenLastCalledWith({ ...EMPTY_QUICK_FILTER, types: ["Bug"] });
    expect(screen.getByRole("button", { name: "Type 1" })).toHaveAttribute("aria-pressed", "true");
  });

  // Writing the address per letter would put every prefix in the history of a replace.
  it("waits for the typing to stop before filtering by text", () => {
    vi.useFakeTimers();
    try {
      const onChange = vi.fn();
      render(<Harness initial={{ ...EMPTY_QUICK_FILTER, mine: true }} hidden={0} onChange={onChange} />);
      const box = screen.getByRole("searchbox");
      for (const value of ["p", "pa", "pay"]) fireEvent.change(box, { target: { value } });
      expect(onChange).not.toHaveBeenCalled();
      act(() => {
        vi.advanceTimersByTime(BOARD_SEARCH_DELAY_MS);
      });
      expect(onChange).toHaveBeenCalledTimes(1);
      expect(onChange).toHaveBeenLastCalledWith({ ...EMPTY_QUICK_FILTER, mine: true, q: "pay" });
    } finally {
      vi.useRealTimers();
    }
  });
});
