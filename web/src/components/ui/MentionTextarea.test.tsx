import { describe, expect, it } from "vitest";
import { mentionMatches, mentionQuery } from "./MentionTextarea";

const people = [
  { id: "1", name: "Ada Lovelace", email: "ada@armature.test" },
  { id: "2", name: "Adam Smith", email: "adam@armature.test" },
  { id: "3", name: "Grace Hopper", email: "grace@armature.test" },
];

describe("mentionQuery", () => {
  it("reads the word after the last at sign before the caret", () => {
    expect(mentionQuery("ping @Ad", 8)).toEqual({ start: 5, query: "Ad" });
    expect(mentionQuery("ping @Ada Lovelace done", 9)).toEqual({ start: 5, query: "Ada" });
  });
  it("is nothing without an at sign, inside an address, or across a line", () => {
    expect(mentionQuery("plain words", 11)).toBeNull();
    expect(mentionQuery("mail ada@armature", 15)).toBeNull();
    expect(mentionQuery("@ada\nnext", 9)).toBeNull();
  });
});

describe("mentionMatches", () => {
  it("offers names and addresses that fit, a few at most", () => {
    expect(mentionMatches(people, "ad").map((p) => p.name)).toEqual(["Ada Lovelace", "Adam Smith"]);
    expect(mentionMatches(people, "grace@").map((p) => p.name)).toEqual(["Grace Hopper"]);
    expect(mentionMatches(people, "zz")).toEqual([]);
  });
  it("offers everyone right after the at sign", () => {
    expect(mentionMatches(people, "")).toHaveLength(3);
  });
});
