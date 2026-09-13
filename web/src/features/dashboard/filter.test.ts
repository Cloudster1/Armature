import { describe, expect, it } from "vitest";
import { EMPTY_FILTER, UNASSIGNED, activeCount, composeQuery, parseSearch, quote, toSearch } from "./filter";

describe("composeQuery", () => {
  it("says nothing for an empty filter", () => {
    expect(composeQuery(EMPTY_FILTER)).toBe("");
    expect(activeCount(EMPTY_FILTER)).toBe(0);
  });

  it("writes each control in the search language and joins them with AND", () => {
    const query = composeQuery({ ...EMPTY_FILTER, types: ["Bug", "Task"], team: "Alpha", assignee: "a@b.c", category: "in_progress", days: 30, field: "resolved" });
    expect(query).toBe('type IN ("Bug", "Task") AND team = "Alpha" AND assignee = "a@b.c" AND statusCategory = in_progress AND resolved >= -30d');
  });

  it("names a milestone the way the search language does", () => {
    expect(composeQuery({ ...EMPTY_FILTER, milestone: "Release 1" })).toBe('milestone = "Release 1"');
  });

  it("uses equals for one type, IS EMPTY for nobody, and brackets the free query", () => {
    expect(composeQuery({ ...EMPTY_FILTER, types: ["Bug"], assignee: UNASSIGNED, query: "priority = high OR labels = urgent" })).toBe(
      'type = "Bug" AND assignee IS EMPTY AND (priority = high OR labels = urgent)',
    );
    expect(composeQuery({ ...EMPTY_FILTER, query: " priority = high " })).toBe("priority = high");
  });

  // A team called 12" Screens must not end the string early.
  it("escapes quotes and backslashes in names", () => {
    expect(quote('12" Screens\\')).toBe('"12\\" Screens\\\\"');
  });
});

describe("the address", () => {
  it("round-trips a filter, and an empty filter still leaves a mark so it beats the saved default", () => {
    const spec = { ...EMPTY_FILTER, types: ["Bug"], team: "Alpha", days: 7, field: "updated" as const, query: "labels = urgent" };
    expect(parseSearch(toSearch(spec) as Record<string, unknown>)).toEqual(spec);
    expect(toSearch(EMPTY_FILTER)).toEqual({ types: undefined, team: undefined, assignee: undefined, category: undefined, field: undefined, days: 0, milestone: undefined, fq: undefined });
    expect(parseSearch(toSearch(EMPTY_FILTER) as Record<string, unknown>)).toEqual(EMPTY_FILTER);
  });

  it("falls back to the saved default when the address says nothing, and ignores nonsense", () => {
    const saved = { ...EMPTY_FILTER, types: ["Task"] };
    expect(parseSearch({}, saved)).toBe(saved);
    expect(parseSearch({ category: "purple", field: "born", days: -4 })).toEqual({ ...EMPTY_FILTER });
  });
});

describe("a saved search on the tile", () => {
  it("is read live through the lookup and ANDed in", () => {
    const spec = { ...EMPTY_FILTER, filterId: "f1", category: "todo" as const };
    expect(composeQuery(spec, (id) => (id === "f1" ? "labels = urgent" : undefined))).toBe("(labels = urgent) AND statusCategory = todo");
    expect(composeQuery(spec)).toBe("statusCategory = todo");
    expect(activeCount(spec)).toBe(2);
    expect(toSearch(spec).sf).toBe("f1");
    expect(parseSearch({ sf: "f1", days: 0 }).filterId).toBe("f1");
  });
});
