import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { useState } from "react";
import { Card, PageHeader, ProgressBar, ShellHeaderContext, Stat } from "./index";

describe("the pieces the shell redesign added", () => {
  it("draws a bar with its aria values and a stat with a share under it", () => {
    render(
      <>
        <ProgressBar value={3} max={4} label="3 of 4 done" segments={[{ value: 3, tone: "done" }]} />
        <Stat label="Schemes" value={2} share={0.5} data-stat="schemes" />
      </>,
    );
    const bar = screen.getByRole("progressbar", { name: "3 of 4 done" });
    expect(bar.getAttribute("aria-valuenow")).toBe("3");
    expect(bar.getAttribute("aria-valuemax")).toBe("4");
    expect(screen.getByRole("progressbar", { name: "Schemes: 50%" })).toBeTruthy();
    expect(document.querySelector('[data-stat="schemes"]')?.textContent).toContain("2");
  });

  it("puts a card's actions in its corner and lifts an elevated one", () => {
    const { container } = render(
      <Card elevated actions={<button type="button">Edit</button>}>
        body
      </Card>,
    );
    expect(container.querySelector("[data-card-actions] button")?.textContent).toBe("Edit");
    expect(container.firstElementChild?.className).toContain("shadow-3");
  });

  // Without a strip the head is drawn in place, which is what every page
  // test and the portal get; with one it is drawn there.
  it("draws the page's head in place, or into the shell's strip when there is one", () => {
    const { container, unmount } = render(<PageHeader title="Alone" />);
    expect(container.querySelector("header h1")?.textContent).toBe("Alone");
    unmount();

    function Shell() {
      const [strip, setStrip] = useState<HTMLElement | null>(null);
      return (
        <div>
          <div ref={setStrip} data-shell-header />
          <ShellHeaderContext.Provider value={strip}>
            <main>
              <PageHeader title="Strip" tabs={<span>tabs</span>} />
              <p>content</p>
            </main>
          </ShellHeaderContext.Provider>
        </div>
      );
    }
    const shell = render(<Shell />);
    expect(shell.container.querySelector("[data-shell-header] header h1")?.textContent).toBe("Strip");
    expect(shell.container.querySelector("main header")).toBeNull();
    expect(shell.container.querySelector("[data-shell-header]")?.textContent).toContain("tabs");
  });
});
