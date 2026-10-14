import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

const start = vi.fn();
const confirm = vi.fn();
const settled = vi.fn();
let started: { enrolment: { secret: string; uri: string } } | undefined;
let status: { enrolled: boolean; enrolledAt?: string; recoveryCodesLeft: number; available: boolean };
const idle = { mutate: vi.fn(), reset: vi.fn(), isPending: false, error: null };

vi.mock("@/api/secondFactor", async () => {
  const actual = await vi.importActual<typeof import("@/api/secondFactor")>("@/api/secondFactor");
  return {
    ...actual,
    useStartSecondFactor: () => ({ ...idle, mutate: start, data: started }),
    useConfirmSecondFactor: () => ({ ...idle, mutate: confirm }),
    useSecondFactorSettled: () => settled,
    useSecondFactorStatus: () => ({ data: { secondFactor: status } }),
    useRenewRecoveryCodes: () => idle,
    useRemoveSecondFactor: () => idle,
    useLoginSecondFactor: () => idle,
  };
});

const logout = vi.fn();
vi.mock("@/api/auth", async () => {
  const actual = await vi.importActual<typeof import("@/api/auth")>("@/api/auth");
  return {
    ...actual,
    useMe: () => ({
      data: {
        principal: { user: { id: "u1" }, installation: { id: "i1", name: "Strict Corp" }, role: "member", mustEnrol: true },
        organizations: [
          { orgId: "o1", orgSlug: "strict", orgName: "Strict Corp", role: "member" },
          { orgId: "o2", orgSlug: "relaxed", orgName: "Relaxed Corp", role: "member" },
        ],
      },
    }),
    useLogout: () => ({ ...idle, mutate: logout }),
  };
});

const { Enrolment, EnrolmentRequired, SecondFactorSettings } = await import("./SecondFactor");

const RECOVERY = Array.from({ length: 10 }, (_, i) => `code-${i}aaa-bbbb-cccc`);

beforeEach(() => {
  vi.clearAllMocks();
  started = undefined;
  status = { enrolled: false, recoveryCodesLeft: 0, available: true };
});

describe("setting up two-step sign-in", () => {
  it("shows the key once started, confirms with a code, then the recovery codes until they are put away", async () => {
    const done = vi.fn();
    const { rerender } = render(<Enrolment onDone={done} />);
    await userEvent.click(screen.getByRole("button", { name: "Set up two-step sign-in" }));
    expect(start).toHaveBeenCalled();

    started = { enrolment: { secret: "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP", uri: "otpauth://totp/Armature:ada?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=Armature" } };
    rerender(<Enrolment onDone={done} />);
    expect(screen.getByRole("img", { name: /QR code/ })).toBeInTheDocument();
    expect(document.querySelector("[data-second-factor-secret]")?.textContent?.replace(/\s/g, "")).toBe(started.enrolment.secret);

    confirm.mockImplementation((_code, options) => options?.onSuccess?.({ recoveryCodes: RECOVERY }));
    await userEvent.type(screen.getByLabelText("Code from the app"), "123456");
    await userEvent.click(screen.getByRole("button", { name: "Turn on two-step sign-in" }));
    expect(confirm.mock.calls[0]?.[0]).toBe("123456");

    const listed = within(document.querySelector("[data-recovery-codes]") as HTMLElement).getAllByRole("listitem");
    expect(listed.map((item) => item.textContent)).toEqual(RECOVERY);
    // Nothing is read back while the codes are on screen, or the page would move on without them.
    expect(settled).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "I have saved them" }));
    expect(settled).toHaveBeenCalled();
    expect(done).toHaveBeenCalled();
  });

  it("says so on the profile when the server has no key to keep a secret with", () => {
    status = { enrolled: false, recoveryCodesLeft: 0, available: false };
    render(<SecondFactorSettings />);
    expect(screen.getByText(/ARMATURE_SECRET_KEY/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Set up two-step sign-in" })).toBeNull();
  });

  it("shows how many recovery codes are left once it is on", () => {
    status = { enrolled: true, enrolledAt: "2026-10-01T09:00:00Z", recoveryCodesLeft: 7, available: true };
    render(<SecondFactorSettings />);
    expect(screen.getByText(/7 recovery codes are left/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Turn off" })).toBeDisabled();
  });
});

describe("a session held to setting it up", () => {
  it("offers the setup and signing out, and nothing else", async () => {
    render(<EnrolmentRequired />);
    expect(screen.getByRole("heading", { name: "Set up two-step sign-in" })).toBeInTheDocument();
    expect(screen.getByText(/Strict Corp asks everybody/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Go to/ })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));
    expect(logout).toHaveBeenCalled();
  });
});
