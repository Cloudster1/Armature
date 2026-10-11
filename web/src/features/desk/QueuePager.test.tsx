import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueuePager, clampQueueOffset, queueCount } from "./QueuePager";

const pageSize = 50;
const total = 250;

describe("QueuePager", () => {
  it("says which requests of the whole queue are shown and goes on to the next", () => {
    const onChange = vi.fn();
    render(<QueuePager total={total} offset={0} pageSize={pageSize} onChange={onChange} />);
    expect(screen.getByText("1 to 50 of 250")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(onChange).toHaveBeenCalledWith(50);
  });

  it("stops at the last page and goes back from it", () => {
    const onChange = vi.fn();
    render(<QueuePager total={total} offset={200} pageSize={pageSize} onChange={onChange} />);
    expect(screen.getByText("201 to 250 of 250")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
    expect(onChange).toHaveBeenCalledWith(150);
  });

  it("is not drawn for a queue that fits on one page", () => {
    const { container } = render(<QueuePager total={pageSize} offset={0} pageSize={pageSize} onChange={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("the queue's count", () => {
  it("is the whole queue, not the page", () => {
    expect(queueCount(total)).toBe("250 requests");
    expect(queueCount(1)).toBe("1 request");
    expect(queueCount(0)).toBe("0 requests");
  });

  it("brings a page that ran past a shrunken queue back to its last page", () => {
    expect(clampQueueOffset(200, 120, pageSize)).toBe(100);
    expect(clampQueueOffset(100, 120, pageSize)).toBe(100);
    expect(clampQueueOffset(50, 0, pageSize)).toBe(0);
  });
});
