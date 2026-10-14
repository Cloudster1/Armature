import { describe, expect, it } from "vitest";
import type { Card } from "@/api/boards";
import type { LabelRef, ParentRef, TypeRef, UserRef } from "@/api/issues";
import { EMPTY_QUICK_FILTER, UNASSIGNED, filterCards, isFiltered, optionsFrom, parseSearch, toSearch, type QuickFilter } from "./quickFilter";

const task: TypeRef = { id: "t1", name: "Task", icon: "task", level: 0, isSubtask: false };
const bug: TypeRef = { id: "t2", name: "Bug", icon: "bug", level: 0, isSubtask: false };
const epicType: TypeRef = { id: "t3", name: "Epic", icon: "epic", level: 1, isSubtask: false };
const ada: UserRef = { id: "u1", name: "Ada" };
const bob: UserRef = { id: "u2", name: "Bob" };
const urgent: LabelRef = { id: "l1", name: "urgent", color: "red" };
const ui: LabelRef = { id: "l2", name: "ui", color: "blue" };
const billing: ParentRef = { id: "e1", key: "CP-1", summary: "Billing", type: epicType };
const login: ParentRef = { id: "e2", key: "CP-2", summary: "Login", type: epicType };

function card(key: string, summary: string, rest: Partial<Card> = {}): Card {
  return {
    id: key,
    key,
    summary,
    type: task,
    labels: [],
    status: { id: "s1", name: "To Do", category: "todo" } as Card["status"],
    priority: "medium",
    rank: "a",
    updatedAt: "2026-01-01T00:00:00Z",
    ...rest,
  };
}

const cards = [
  card("CP-10", "Pay by card", { assignee: ada, labels: [urgent], parent: billing }),
  card("CP-11", "Refund a payment", { assignee: bob, type: bug, labels: [urgent, ui], parent: billing }),
  card("CP-12", "Sign in with a passkey", { assignee: ada, parent: login }),
  card("CP-13", "Fix the footer", { type: bug, labels: null as unknown as LabelRef[] }),
];

const keys = (filter: Partial<QuickFilter>, me = "u1") => filterCards(cards, { ...EMPTY_QUICK_FILTER, ...filter }, me).map((c) => c.key);

describe("filterCards", () => {
  it("keeps every card when nothing is chosen", () => {
    expect(keys({})).toEqual(["CP-10", "CP-11", "CP-12", "CP-13"]);
    expect(isFiltered(EMPTY_QUICK_FILTER)).toBe(false);
  });

  it("narrows to the cards of the person signed in", () => {
    expect(keys({ mine: true })).toEqual(["CP-10", "CP-12"]);
    expect(keys({ mine: true }, "u2")).toEqual(["CP-11"]);
  });

  it("narrows by assignee, with the unassigned value matching cards nobody holds", () => {
    expect(keys({ assignees: ["u2"] })).toEqual(["CP-11"]);
    expect(keys({ assignees: [UNASSIGNED] })).toEqual(["CP-13"]);
    expect(keys({ assignees: ["u2", UNASSIGNED] })).toEqual(["CP-11", "CP-13"]);
  });

  it("narrows by label, type and epic", () => {
    expect(keys({ labels: ["l2"] })).toEqual(["CP-11"]);
    expect(keys({ types: ["Bug"] })).toEqual(["CP-11", "CP-13"]);
    expect(keys({ epics: ["CP-2"] })).toEqual(["CP-12"]);
  });

  it("matches the text against the key and the summary, ignoring case", () => {
    expect(keys({ q: "  PAY " })).toEqual(["CP-10", "CP-11"]);
    expect(keys({ q: "cp-13" })).toEqual(["CP-13"]);
  });

  it("ORs the values within a chip and ANDs the chips together", () => {
    expect(keys({ epics: ["CP-1", "CP-2"] })).toEqual(["CP-10", "CP-11", "CP-12"]);
    expect(keys({ epics: ["CP-1", "CP-2"], types: ["Task"] })).toEqual(["CP-10", "CP-12"]);
    expect(keys({ mine: true, labels: ["l1"] })).toEqual(["CP-10"]);
    expect(keys({ mine: true, labels: ["l1"], q: "refund" })).toEqual([]);
  });

  it("finds nothing of mine for nobody signed in", () => {
    expect(keys({ mine: true }, "")).toEqual([]);
  });
});

describe("optionsFrom", () => {
  it("offers only what is on the board, once each, in name order", () => {
    const options = optionsFrom(cards);
    expect(options.assignees.map((a) => a.id)).toEqual(["u1", "u2", UNASSIGNED]);
    expect(options.labels.map((l) => l.name)).toEqual(["ui", "urgent"]);
    expect(options.types.map((t) => t.name)).toEqual(["Bug", "Task"]);
    expect(options.epics.map((e) => e.key)).toEqual(["CP-1", "CP-2"]);
  });

  it("offers no unassigned when every card is held", () => {
    expect(optionsFrom(cards.slice(0, 3)).assignees.map((a) => a.id)).toEqual(["u1", "u2"]);
  });
});

describe("the address", () => {
  it("round-trips a filter", () => {
    const filter: QuickFilter = { mine: true, assignees: ["u2", UNASSIGNED], labels: ["l1"], types: ["Bug", "Task"], epics: ["CP-1"], q: "pay" };
    expect(parseSearch(toSearch(filter) as Record<string, unknown>)).toEqual(filter);
    expect(parseSearch(toSearch(EMPTY_QUICK_FILTER) as Record<string, unknown>)).toEqual(EMPTY_QUICK_FILTER);
  });

  it("writes nothing for an empty filter", () => {
    expect(Object.values(toSearch(EMPTY_QUICK_FILTER)).filter((value) => value !== undefined)).toEqual([]);
  });

  it("ignores what is not one of its values, and reads a number typed into the text", () => {
    expect(parseSearch({ mine: "yes", assignee: 3, label: ",,l1,", q: 42, other: "x" })).toEqual({ ...EMPTY_QUICK_FILTER, labels: ["l1"], q: "42" });
  });
});
