import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const provider = {
  issuer: "https://id.example.test/realms/dynamo",
  clientId: "armature",
  hasSecret: true,
  groupsClaim: "groups",
  scopes: "openid profile email",
  createGroups: false,
  admitByGroup: false,
  enabled: true,
  updatedAt: "2026-10-19T00:00:00Z",
};
const saved: unknown[] = [];
vi.mock("@/api/access", () => ({
  useProvider: () => ({ data: { provider }, isLoading: false }),
  useSaveProvider: () => ({ mutate: (input: unknown) => saved.push(input), isPending: false, error: null }),
}));

const { ProviderSettings } = await import("./ProviderSettings");

describe("ProviderSettings", () => {
  // Off unless an administrator turns it on, and the page says what it does.
  it("lets people in by their groups only once it is ticked and saved", async () => {
    render(<ProviderSettings />);
    const admit = screen.getByRole("checkbox", { name: "Let people in by their groups" }) as HTMLInputElement;
    expect(admit.checked).toBe(false);
    expect(screen.getByText(/whose group from the provider holds a role is let in/)).toBeTruthy();

    await userEvent.click(admit);
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(saved.at(-1)).toMatchObject({ admitByGroup: true, enabled: true, clientSecret: "" });

    await userEvent.click(screen.getByRole("button", { name: "Turn it off" }));
    expect(saved.at(-1)).toMatchObject({ admitByGroup: false, enabled: false });
  });
});
