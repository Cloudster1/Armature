import { describe, expect, it } from "vitest";
import type { Project } from "@/api/projects";
import { featureOfPath, hasFeature, leftOut } from "./features";
import { pagesFor, projectSetupPages, projectWorkPages } from "@/features/shell/Sidebar";
import { answer } from "@/features/guide/match";

const desk = {
  key: "HELP",
  kind: "service",
  features: ["board", "calendar", "dashboard", "queues", "desk", "teams", "automation", "import"],
} as Project;

describe("a project's features", () => {
  it("decide which pages the sidebar lists", () => {
    const labels = pagesFor(desk, [...projectWorkPages, ...projectSetupPages]).map((page) => page.label);
    expect(labels).toContain("Queues");
    expect(labels).toContain("Issues");
    expect(labels).toContain("Settings");
    expect(labels).not.toContain("Sprints");
    expect(labels).not.toContain("Repositories");
  });

  // A project still loading shows everything rather than flashing a shorter list.
  it("are given the benefit of the doubt until the project is known", () => {
    expect(pagesFor(undefined, projectWorkPages)).toHaveLength(projectWorkPages.length);
    expect(hasFeature(undefined, "sprints")).toBe(true);
  });

  it("name the page a path under the project belongs to", () => {
    expect(featureOfPath("HELP", "/projects/HELP/sprints")).toBe("sprints");
    expect(featureOfPath("HELP", "/projects/HELP/sprints/abc/board")).toBe("sprints");
    expect(featureOfPath("HELP", "/projects/HELP/service-desk")).toBe("desk");
    expect(featureOfPath("HELP", "/projects/HELP/settings")).toBeUndefined();
    expect(featureOfPath("HELP", "/projects/HELP")).toBeUndefined();
  });

  it("read on a template card as what it leaves out", () => {
    expect(leftOut(desk.features)).toEqual(["sprints", "plan", "milestones", "releases", "components", "hierarchy", "repositories"]);
    expect(leftOut(["board", "sprints", "plan", "calendar", "milestones", "releases", "components", "hierarchy", "dashboard", "teams", "repositories", "automation", "import"])).toEqual([]);
  });

  it("keep the guide from answering with a page the project lacks", () => {
    const withAll = answer("where are the sprints", { projectKey: "HELP", canAdministerOrg: true });
    const withDesk = answer("where are the sprints", { projectKey: "HELP", canAdministerOrg: true, features: desk.features });
    expect(withAll.some((a) => a.entry.id === "page:/sprints")).toBe(true);
    expect(withDesk.some((a) => a.entry.id === "page:/sprints")).toBe(false);
  });
});
