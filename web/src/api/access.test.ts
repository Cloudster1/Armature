import { describe, expect, it } from "vitest";
import { canAdminister, canWriteIssues, type Access } from "./access";

function access(over: Partial<Access>): Access {
  return { grants: [], projects: [], canAdministerOrg: false, canCreateProject: false, permissions: { org: [], projects: {} }, ...over };
}

describe("grants", () => {
  it("knows nothing until the access has loaded", () => {
    expect(canAdminister(undefined, "CP")).toBe(false);
    expect(canWriteIssues(undefined, "CP")).toBe(false);
  });

  it("lets an organization administrator do everything everywhere", () => {
    const a = access({ canAdministerOrg: true, permissions: { org: ["read", "issue.write", "project.administer", "org.administer"], projects: {} } });
    expect(canAdminister(a, "CP")).toBe(true);
    expect(canWriteIssues(a, "ZZ")).toBe(true);
  });

  // A grant over one project says nothing about another.
  it("keeps a project scoped grant to its project", () => {
    const a = access({ grants: [{ role: "project_administrator", projectKey: "CP" }], permissions: { org: [], projects: { CP: ["read", "issue.write", "project.administer"] } } });
    expect(canAdminister(a, "CP")).toBe(true);
    expect(canAdminister(a, "OPS")).toBe(false);
    expect(canWriteIssues(a, "OPS")).toBe(false);
  });

  it("applies an organization wide grant to every project", () => {
    const a = access({ grants: [{ role: "user" }], permissions: { org: ["read", "issue.write"], projects: {} } });
    expect(canWriteIssues(a, "CP")).toBe(true);
    expect(canAdminister(a, "CP")).toBe(false);
  });

  it("gives a reader no way to change anything", () => {
    const a = access({ grants: [{ role: "reader", projectKey: "CP" }], permissions: { org: [], projects: { CP: ["read"] } } });
    expect(canWriteIssues(a, "CP")).toBe(false);
    expect(canAdminister(a, "CP")).toBe(false);
  });
});
