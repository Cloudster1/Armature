import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { RemoteLink } from "@/api/remoteLinks";

const removeMutate = vi.fn();
let remoteLinks: RemoteLink[] = [];
vi.mock("@/api/remoteLinks", () => ({
  useRemoteLinks: () => ({ data: { remoteLinks }, isLoading: false }),
  useRemoveRemoteLink: () => ({ mutate: removeMutate, error: null, isPending: false }),
}));

import { PagesPanel } from "./PagesPanel";

const spec: RemoteLink = {
  id: "0199a0b2-0000-7000-8000-000000000001",
  url: "https://wiki.example.com/pages/checkout",
  title: "Checkout spec",
  source: "Stator",
  iconUrl: "https://wiki.example.com/favicon.png",
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-01T00:00:00Z",
};

describe("PagesPanel", () => {
  it("is not there at all while the issue has no pages", () => {
    remoteLinks = [];
    const { container } = render(<PagesPanel issueKey="CP-1" editable />);
    expect(container).toBeEmptyDOMElement();
  });

  it("lists each page with its icon and source, opening it in a new tab", () => {
    remoteLinks = [spec];
    render(<PagesPanel issueKey="CP-1" editable={false} />);
    const link = screen.getByRole("link", { name: "Checkout spec" });
    expect(link).toHaveAttribute("href", spec.url);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
    expect(screen.getByText("Stator")).toBeInTheDocument();
    expect(document.querySelector("[data-remote-link-icon]")).toHaveAttribute("src", spec.iconUrl);
    expect(screen.queryByRole("button", { name: /Remove/ })).not.toBeInTheDocument();
  });

  it("stands the source's initial in for an icon that will not load", () => {
    remoteLinks = [spec];
    render(<PagesPanel issueKey="CP-1" editable={false} />);
    fireEvent.error(document.querySelector("[data-remote-link-icon]")!);
    expect(document.querySelector("[data-remote-link-icon]")).toBeNull();
    expect(screen.getByText("S")).toBeInTheDocument();
  });

  it("lets somebody who edits the issue take a page off", () => {
    remoteLinks = [spec];
    render(<PagesPanel issueKey="CP-1" editable />);
    fireEvent.click(screen.getByRole("button", { name: "Remove the page Checkout spec" }));
    expect(removeMutate).toHaveBeenCalledWith({ key: "CP-1", id: spec.id });
  });
});
