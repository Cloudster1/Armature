import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ApiError } from "@/api/client";

const requestMutate = vi.fn();
const enterMutate = vi.fn();
let enterError: Error | null = null;
let openError: Error | null = null;
vi.mock("@/api/desk", () => ({
  useRequestCode: () => ({ mutate: requestMutate, error: null, isPending: false, reset: vi.fn() }),
  useEnterWithCode: () => ({ mutate: enterMutate, error: enterError, isPending: false, reset: vi.fn() }),
  useEnterOpenDoor: () => ({ mutate: vi.fn(), error: openError, isPending: false, reset: vi.fn() }),
}));

import { DeskEntryForm, OpenDoorForm } from "./DeskEntryForm";

describe("DeskEntryForm", () => {
  it("asks for the address, then for the code that was mailed to it", () => {
    requestMutate.mockImplementation((_input, options) => options.onSuccess());
    render(<DeskEntryForm slug="acme" deskName="Acme support" onEntered={() => {}} />);
    expect(screen.getByText(/Acme support answers requests here/)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "ada@example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Send me a code" }));
    expect(requestMutate).toHaveBeenCalledWith({ slug: "acme", email: "ada@example.com" }, expect.anything());

    expect(screen.getByText("ada@example.com")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Code"), { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: "Enter" }));
    expect(enterMutate).toHaveBeenCalledWith({ slug: "acme", email: "ada@example.com", code: "123456" }, expect.anything());
  });

  it("shows the server's sentence when the code is refused", () => {
    enterError = new ApiError(401, { code: "code_invalid", message: "That code is wrong or has expired. Ask for a new one." });
    requestMutate.mockImplementation((_input, options) => options.onSuccess());
    render(<DeskEntryForm slug="acme" deskName="Acme support" onEntered={() => {}} />);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "ada@example.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Send me a code" }));
    expect(screen.getByText("That code is wrong or has expired. Ask for a new one.")).toBeInTheDocument();
  });

  it("says which domains an open door takes when an address is turned away", () => {
    openError = new ApiError(403, { code: "domain_not_trusted", message: "This desk takes requests from addresses at armature.test only." });
    render(<OpenDoorForm slug="acme" door={{ key: "ITD", name: "IT desk" }} onEntered={() => {}} />);
    expect(screen.getByRole("alert")).toHaveTextContent("addresses at armature.test only");
  });
});
