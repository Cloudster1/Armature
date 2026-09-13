import { describe, expect, it } from "vitest";
import type { RequestType } from "@/api/desk";
import { categoriesOf, groupByCategory, prefillDetails } from "./templates";

function type(name: string, category: string): RequestType {
  return {
    id: name,
    projectId: "p",
    projectKey: "HLP",
    name,
    issueTypeId: "t",
    issueTypeName: "Task",
    priority: "medium",
    position: 0,
    category,
    detailsTemplate: "",
  };
}

describe("groupByCategory", () => {
  it("puts the general list first and keeps categories in the order they are met", () => {
    const groups = groupByCategory([type("Printer", "Hardware"), type("Question", ""), type("VPN", "Access"), type("Laptop", "hardware")]);
    expect(groups.map((g) => g.name)).toEqual(["General", "Hardware", "Access"]);
    expect(groups[1]?.types.map((t) => t.name)).toEqual(["Printer", "Laptop"]);
  });

  it("has no general heading when every type has a category", () => {
    expect(groupByCategory([type("Printer", "Hardware")]).map((g) => g.name)).toEqual(["Hardware"]);
  });

  it("lists the categories in use without the general one", () => {
    expect(categoriesOf([type("Question", ""), type("VPN", "Access"), type("Laptop", "Hardware")])).toEqual(["Access", "Hardware"]);
  });
});

describe("prefillDetails", () => {
  it("fills an empty box with the template", () => {
    expect(prefillDetails("", "", "Device:\nWhat happened:")).toBe("Device:\nWhat happened:");
  });

  it("replaces a template the requester has not touched", () => {
    expect(prefillDetails("Device:", "Device:", "Which system:")).toBe("Which system:");
  });

  it("never overwrites what the requester typed", () => {
    expect(prefillDetails("Device: my laptop", "Device:", "Which system:")).toBe("Device: my laptop");
  });
});
