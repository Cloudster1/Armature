import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { JoinRequests } from "./JoinRequests";

const requests = vi.fn();
const admit = vi.fn();
const decline = vi.fn();
vi.mock("@/api/users", () => ({
	useJoinRequests: () => ({ data: { requests: requests() } }),
	useAdmitJoinRequest: () => ({ mutate: admit, isPending: false, error: null }),
	useDeclineJoinRequest: () => ({
		mutate: decline,
		isPending: false,
		error: null,
	}),
}));
vi.mock("@/lib/format", () => ({ useFormat: () => ({ dateTime: (iso: string) => `at ${iso}` }) }));
vi.mock("@/components/ui", async () => {
	const actual =
		await vi.importActual<typeof import("@/components/ui")>("@/components/ui");
	return {
		...actual,
		useToast: () => ({ success: vi.fn(), info: vi.fn(), error: vi.fn() }),
	};
});

const waiting = {
	userId: "u9",
	email: "stranger@example.test",
	name: "A Stranger",
	requestedAt: "2026-09-28T10:00:00Z",
};

describe("the people waiting to be let in", () => {
	beforeEach(() => vi.clearAllMocks());

	it("take no room when there are none", () => {
		requests.mockReturnValue([]);
		const { container } = render(<JoinRequests />);
		expect(container.querySelector("[data-join-requests]")).toBeNull();
	});

	it("are listed by name and address, and let in as a member with one click", async () => {
		requests.mockReturnValue([waiting]);
		render(<JoinRequests />);
		expect(screen.getByText("A Stranger")).toBeTruthy();
		expect(screen.getByText(/stranger@example\.test/)).toBeTruthy();

		await userEvent.click(
			screen.getByRole("button", { name: "Let in as member" }),
		);
		expect(admit).toHaveBeenCalledWith(
			{ userId: "u9", role: "member" },
			expect.anything(),
		);

		await userEvent.click(screen.getByRole("button", { name: "Turn away" }));
		expect(decline).toHaveBeenCalledWith({ userId: "u9" });
	});
});
