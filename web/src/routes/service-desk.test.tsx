import { afterEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import { renderInShell, serve } from "@/test/shell";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  createRoute: (options: object) => ({ ...options, useParams: () => ({ projectKey: "CP" }) }),
}));
vi.mock("./project", () => ({ projectRoute: {} }));

const { RequestTypes } = await import("./service-desk");

function type(id: string, name: string, level: number) {
  return { id, name, icon: "task", level, isSubtask: level < 0, position: 0, builtin: true, retired: false, issues: 0 };
}

afterEach(() => vi.unstubAllGlobals());

describe("the request-type form of the service desk", () => {
  // The library has Story and Bug too; this desk's project allows Task and Incident.
  it("offers only the issue types of the project, and none under the standard level", async () => {
    serve({
      "GET /projects/CP/request-types": [200, { requestTypes: [] }],
      "GET /projects/CP/teams": [200, { teams: [] }],
      "GET /projects/CP/issue-types": [200, { issueTypes: [type("task", "Task", 0), type("incident", "Incident", 0), type("sub", "Subtask", -1)] }],
    });
    renderInShell(
      <ConfirmProvider>
        <RequestTypes projectKey="CP" />
      </ConfirmProvider>,
    );

    const select = await screen.findByLabelText("Becomes");
    await vi.waitFor(() => expect([...select.querySelectorAll("option")].map((o) => o.textContent)).toContain("Task"));
    expect([...select.querySelectorAll("option")].map((o) => o.textContent)).toEqual(["The first issue type", "Task", "Incident"]);
  });
});
