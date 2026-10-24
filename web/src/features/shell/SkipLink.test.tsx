import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SkipLink } from "./SkipLink";
import { MAIN_ID } from "./state";

describe("the skip link", () => {
  it("is the first thing the keyboard reaches, and moves focus to the content without changing the address", async () => {
    render(
      <>
        <SkipLink />
        <nav>
          <a href="/">Home</a>
        </nav>
        <main id={MAIN_ID} tabIndex={-1}>
          The page
        </main>
      </>,
    );
    const before = window.location.href;
    await userEvent.tab();
    const link = screen.getByRole("link", { name: "Skip to the content" });
    expect(document.activeElement).toBe(link);
    await userEvent.keyboard("{Enter}");
    expect(document.activeElement).toBe(screen.getByRole("main"));
    expect(window.location.href).toBe(before);
  });
});
