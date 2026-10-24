import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Button, ToastProvider, ToastShelf, useToast } from "./index";

function Saver() {
  const toast = useToast();
  return <Button onClick={() => toast.success("Profile saved")}>Save</Button>;
}

describe("the toast shelf", () => {
  // A change of language draws the tree under the router again; what was just said stays said.
  it("keeps a toast on screen when the provider under it is drawn again from scratch", async () => {
    const page = (generation: number) => (
      <ToastShelf>
        <ToastProvider key={generation}>
          <Saver />
        </ToastProvider>
      </ToastShelf>
    );
    const { rerender } = render(page(1));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(screen.getByRole("status")).toHaveTextContent("Profile saved");

    rerender(page(2));
    expect(screen.getByRole("status")).toHaveTextContent("Profile saved");
  });

  it("loses it without a shelf, which is why the app keeps one above the router", async () => {
    const page = (generation: number) => (
      <ToastProvider key={generation}>
        <Saver />
      </ToastProvider>
    );
    const { rerender } = render(page(1));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    rerender(page(2));
    expect(screen.queryByRole("status")).toBeNull();
  });
});
