import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Count } from "@/components/ui";
import { onApple } from "@/lib/device";
import { crumbsFor, projectOfPath } from "./Crumbs";

const labels = (pathname: string, projectName?: string) => crumbsFor(pathname, { projectName }).map((crumb) => `${crumb.label} ${crumb.to}`);

describe("the trail a page header draws", () => {
  it("leads nowhere from the places at the top", () => {
    for (const path of ["/", "/projects", "/search", "/notifications", "/settings", "/admin"]) expect(labels(path)).toEqual([]);
  });

  it("passes Projects and the project on a project's page and on an issue", () => {
    expect(labels("/projects/ALP")).toEqual(["Projects /projects"]);
    expect(labels("/projects/ALP/board", "Alpha")).toEqual(["Projects /projects", "Alpha /projects/ALP"]);
    expect(labels("/issues/ALP-12", "Alpha")).toEqual(["Projects /projects", "Alpha /projects/ALP"]);
    expect(projectOfPath("/issues/MY-APP-3")).toBe("MY-APP");
  });

  // The settings' crumbs are the project, then Settings, on every tab but General.
  it("passes the project's settings on one of their tabs", () => {
    expect(labels("/projects/ALP/settings", "Alpha")).toEqual(["Projects /projects", "Alpha /projects/ALP"]);
    expect(labels("/projects/ALP/settings/fields", "Alpha")).toEqual(["Projects /projects", "Alpha /projects/ALP", "Project settings /projects/ALP/settings"]);
  });

  it("passes Settings below the person's pages and Admin below the installation's", () => {
    expect(labels("/settings/tokens")).toEqual(["Settings /settings"]);
    expect(labels("/settings/themes/new")).toEqual(["Settings /settings", "Themes /settings/themes"]);
    expect(labels("/admin/access")).toEqual(["Admin /admin"]);
    expect(labels("/admin/workflows/schemes")).toEqual(["Admin /admin", "Workflows /admin/workflows"]);
    expect(labels("/filters/f1")).toEqual(["Saved searches /filters"]);
  });

  it("leads a customer back to their requests", () => {
    expect(labels("/portal/requests/DESK-1")).toEqual(["Your requests /portal"]);
    expect(labels("/portal/articles/a1")).toEqual(["Raise a request /portal/new"]);
  });
});

describe("Count", () => {
  it("caps at 99+, says the whole number, and draws nothing for none", () => {
    const { container, rerender } = render(<Count count={120} name="notifications" says="120 unread" />);
    expect(container.querySelector('[data-count="notifications"]')?.textContent).toBe("99+, 120 unread");
    expect(screen.getByText("99+")).toHaveAttribute("aria-hidden", "true");
    rerender(<Count count={0} name="notifications" says="none" />);
    expect(container.querySelector("[data-count]")).toBeNull();
  });
});

describe("the palette's key", () => {
  it("is Cmd on an Apple system and Ctrl elsewhere", () => {
    expect(onApple("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 Version/17.5 Safari/605.1.15")).toBe(true);
    expect(onApple("Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X)")).toBe(true);
    expect(onApple("Mozilla/5.0 (X11; Linux x86_64) Chrome/130.0 Safari/537.36")).toBe(false);
  });
});
