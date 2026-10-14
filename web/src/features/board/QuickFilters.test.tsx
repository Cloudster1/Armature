import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { BOARD_SEARCH_DELAY_MS } from "@/config";
import { QuickFilters } from "./QuickFilters";
import { EMPTY_QUICK_FILTER, type QuickFilter, type QuickFilterOptions } from "./quickFilter";

const options: QuickFilterOptions = {
  assignees: [{ id: "u1", name: "Ada" }],
  labels: [],
  types: [{ id: "t1", name: "Bug", icon: "bug", level: 0, isSubtask: false }],
  epics: [],
};

function Harness({ initial, hidden, onChange }: { initial: QuickFilter; hidden: number; onChange: (filter: QuickFilter) => void }) {
  const [filter, setFilter] = useState(initial);
  return (
    <QuickFilters
      filter={filter}
      options={options}
      hidden={hidden}
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
