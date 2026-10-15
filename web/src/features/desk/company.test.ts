import { describe, expect, it } from "vitest";
import type { PortalRequest } from "@/api/desk";
import { companyLine, parseDomains, sharedByCompany } from "./company";

describe("a desk's companies", () => {
  it("reads domains typed as a list the way the server stores them", () => {
    expect(parseDomains(" @Globex.test, globex.test  initech.test,,")).toEqual(["globex.test", "initech.test"]);
    expect(parseDomains("")).toEqual([]);
  });

  it("says whether a request was shared with its company", () => {
    expect(companyLine("Globex", true)).toBe("Globex, shared");
    expect(companyLine("Globex", false)).toBe("Globex");
  });

  it("groups shared requests by company, in the order they came", () => {
    const of = (key: string, company: string) => ({ key, company: { id: company, name: company } }) as PortalRequest;
    const groups = sharedByCompany([of("A-1", "Globex"), of("A-2", "Initech"), of("A-3", "Globex")]);
    expect(groups.map((g) => [g.company, g.requests.map((r) => r.key)])).toEqual([
      ["Globex", ["A-1", "A-3"]],
      ["Initech", ["A-2"]],
    ]);
  });
});
