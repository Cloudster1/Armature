import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import type { Company } from "@/api/desk";

const create = vi.fn();
const addMember = vi.fn();
const companies: Company[] = [
  {
    id: "c1", projectId: "p", projectKey: "HLP", name: "Globex", domains: ["globex.test"], createdAt: "2026-10-10T08:00:00Z",
    members: [{ userId: "u1", name: "Ada", email: "ada@globex.test", addedAt: "2026-10-10T08:00:00Z" }],
  },
];
vi.mock("@/api/desk", () => ({
  useCompanies: () => ({ data: { companies } }),
  useCreateCompany: () => ({ mutate: create, isPending: false, error: null }),
  useUpdateCompany: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  useDeleteCompany: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  useAddCompanyMember: () => ({ mutate: addMember, isPending: false, error: null }),
  useRemoveCompanyMember: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => async () => true }));
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), error: vi.fn() }) };
});

import { Companies } from "./Companies";

describe("the Companies section", () => {
  it("lists each company with its domains and customers", () => {
    const { container } = render(<Companies projectKey="HLP" editable={false} />);
    const card = container.querySelector('[data-company="Globex"]') as HTMLElement;
    expect(within(card).getByText("globex.test")).toBeInTheDocument();
    expect(within(card).getByText("ada@globex.test")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add company" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Add customer" })).toBeNull();
  });

  it("lets an administrator add a company with its domains, and a customer by address", () => {
    render(<Companies projectKey="HLP" editable />);
    fireEvent.change(screen.getByLabelText("Company"), { target: { value: " Initech " } });
    fireEvent.change(screen.getByLabelText("Domains"), { target: { value: "initech.test, @Initech.example" } });
    fireEvent.click(screen.getByRole("button", { name: "Add company" }));
    expect(create).toHaveBeenCalledWith({ name: "Initech", domains: ["initech.test", "initech.example"] }, expect.anything());

    fireEvent.change(screen.getByLabelText("A customer for Globex"), { target: { value: "bob@globex.test" } });
    fireEvent.click(screen.getByRole("button", { name: "Add customer" }));
    expect(addMember).toHaveBeenCalledWith({ id: "c1", email: "bob@globex.test" }, expect.anything());
  });
});
