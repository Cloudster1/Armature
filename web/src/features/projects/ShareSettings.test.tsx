import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Allocation } from "@/api/resources";

const mutate = vi.fn();
let allocations: Allocation[] = [];
vi.mock("@/api/resources", async () => {
  const actual = await vi.importActual<typeof import("@/api/resources")>("@/api/resources");
  return {
    ...actual,
    useAllocations: () => ({ data: { allocations }, error: null }),
    useSetAllocation: () => ({ mutate, isPending: false, error: null }),
  };
});
vi.mock("@/components/ui", async () => {
  const actual = await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
  return { ...actual, useToast: () => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }) };
});

const { ShareSettings } = await import("./ShareSettings");

function shares(list: Allocation[]) {
  allocations = list;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ShareSettings projectKey="FLOW" />
    </QueryClientProvider>,
  );
}

describe("the share of the week", () => {
  beforeEach(() => vi.clearAllMocks());

  it("lists the project's people at their share and saves a new one", async () => {
    shares([
      { userId: "ada", name: "Ada", percent: 100, elsewherePercent: 0 },
      { userId: "bea", name: "Bea", percent: 40, elsewherePercent: 0 },
    ]);
    const ada = screen.getByRole("spinbutton", { name: "Ada's share of the week" });
    expect(ada).toHaveValue(100);
    expect(screen.getByRole("spinbutton", { name: "Bea's share of the week" })).toHaveValue(40);
    await userEvent.clear(ada);
    await userEvent.type(ada, "50");
    await userEvent.click(screen.getByRole("button", { name: "Save Ada's share" }));
    expect(mutate).toHaveBeenCalledWith({ userId: "ada", percent: 50 }, expect.anything());
  });

  it("warns, and never refuses, when somebody gives more than their whole week", async () => {
    shares([{ userId: "ada", name: "Ada", percent: 60, elsewherePercent: 70 }]);
    expect(screen.getByText("Ada gives 130% of their week across projects.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save Ada's share" })).toBeDisabled();
    await userEvent.type(screen.getByRole("spinbutton", { name: "Ada's share of the week" }), "{backspace}{backspace}20");
    expect(screen.queryByText(/across projects/)).not.toBeInTheDocument();
  });

  it("says when the project has nobody to share out", () => {
    shares([]);
    expect(screen.getByText(/Nobody is on the project's teams/)).toBeInTheDocument();
  });
});
