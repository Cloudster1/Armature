import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { Issue } from "@/api/issues";
import { refusal, renderInShell, serve } from "@/test/shell";
import { AssigneeField, ReporterField } from "./PeopleFields";

const issue = {
  id: "i1",
  key: "CP-4",
  summary: "Fix the login loop",
  projectKey: "CP",
  priority: "medium",
  reporter: { id: "ada", name: "Ada" },
} as Issue;

const members = {
  members: [
    { id: "ada", name: "Ada", email: "ada@example.com", role: "member" },
    { id: "ben", name: "Ben", email: "ben@example.com", role: "member" },
  ],
};

const notAMember = "That person is not a member here.";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("people on an issue", () => {
  it("says why the server refused an assignee", async () => {
    serve({ "GET /members": [200, members], "PATCH /issues/CP-4": [404, refusal("not_found", notAMember)] });
    renderInShell(<AssigneeField issue={issue} editable />);

    await screen.findByRole("option", { name: "Ben" });
    fireEvent.change(screen.getByLabelText("Assignee"), { target: { value: "ben" } });

    expect(await screen.findByRole("alert")).toHaveTextContent(notAMember);
  });

  it("says why the server refused a reporter", async () => {
    serve({ "GET /members": [200, members], "PATCH /issues/CP-4": [404, refusal("not_found", notAMember)] });
    renderInShell(<ReporterField issue={issue} editable />);

    await screen.findByRole("option", { name: "Ben" });
    fireEvent.change(screen.getByLabelText("Reporter"), { target: { value: "ben" } });

    expect(await screen.findByRole("alert")).toHaveTextContent(notAMember);
  });
});
