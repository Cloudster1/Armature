import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { axeViolations } from "./axe";

// A check that never fails proves nothing: a planted fault is named.
describe("the accessibility check", () => {
  it("names a button without a name and an image without a text", async () => {
    render(
      <main>
        <h1>Planted</h1>
        <button type="button" data-planted />
        <img src="/mark-96.webp" />
      </main>,
    );
    const found = await axeViolations();
    expect(found.some((v) => v.startsWith("button-name:"))).toBe(true);
    expect(found.some((v) => v.startsWith("image-alt:"))).toBe(true);
  });
});
