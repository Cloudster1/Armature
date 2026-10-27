import assert from "node:assert/strict";
import { test } from "node:test";

import { mergeShards, parseShard, shardOf } from "./shard.mjs";

const names = (list) => list.map((s) => s.name);

/** A registered suite of three files, each with a few scenarios, one of them serial. */
const suite = ["board", "desk", "work"].flatMap((file) =>
  ["first", "second", "third"].map((name, i) => ({ file: `${file}.mjs`, name: `${file} ${name}`, serial: i === 2 })),
);

test("every scenario lands in exactly one shard, and one file's spread across them", () => {
  const count = 4;
  const parts = Array.from({ length: count }, (_, i) => shardOf(suite, { index: i + 1, count }));
  assert.deepEqual(names(parts.flat()).sort(), names(suite).sort());
  for (const part of parts) assert.ok(part.length >= 2 && part.length <= 3, `a shard of ${part.length}`);
  assert.ok(new Set(parts[0].map((s) => s.file)).size > 1, "the first shard holds more than one file");
});

test("the split does not depend on the order the files register in", () => {
  const shard = { index: 2, count: 3 };
  assert.deepEqual(names(shardOf(suite, shard)).sort(), names(shardOf([...suite].reverse(), shard)).sort());
});

test("a shard keeps the order its scenarios were registered in", () => {
  const part = shardOf(suite, { index: 1, count: 2 });
  assert.deepEqual(names(part), names(suite.filter((s) => part.includes(s))));
});

test("no shard asked for means the whole run", () => {
  assert.equal(parseShard(""), null);
  assert.equal(parseShard(undefined), null);
  assert.equal(shardOf(suite, null), suite);
  assert.deepEqual(parseShard("3/4"), { index: 3, count: 4 });
});

test("a shard written any other way than i/n is refused with how to write it", () => {
  for (const bad of ["0/4", "5/4", "1/0", "2", "a/b", "1/4/2", "-1/4"]) {
    assert.throws(() => parseShard(bad), /write it as i\/n with i from 1 to n/, bad);
  }
});

/** What one job's timings.json says, for the scenarios it ran. */
function ran(shard, of, ...scenarioNames) {
  return { shard, of, seconds: 100, scenarios: scenarioNames.map((name) => ({ name, file: "x.mjs", ok: name !== "broken", ms: 10 })) };
}

test("the shards' timings add up to the suite once", () => {
  const { problems, scenarios, shards } = mergeShards([ran("2/2", 3, "b", "broken"), ran("1/2", 3, "a")]);
  assert.deepEqual(problems, []);
  assert.deepEqual(names(scenarios), ["a", "b", "broken"]);
  assert.deepEqual(
    shards.map((s) => [s.shard, s.ran, s.failed]),
    [
      ["1/2", 1, []],
      ["2/2", 2, ["broken"]],
    ],
  );
});

test("a missing shard, a scenario run twice and one never run are each named", () => {
  assert.match(mergeShards([ran("1/3", 2, "a"), ran("3/3", 2, "b")]).problems.join(" "), /Shard 2\/3 left no timings/);
  assert.match(mergeShards([ran("1/2", 2, "a"), ran("2/2", 2, "a")]).problems.join(" "), /"a" ran in 1\/2 and 2\/2/);
  assert.match(mergeShards([ran("1/2", 3, "a"), ran("2/2", 3, "b")]).problems.join(" "), /ran 2 scenarios, but the suite has 3/);
  assert.match(mergeShards([]).problems.join(" "), /No job left its timings/);
  assert.match(mergeShards([ran(null, 1, "a")]).problems.join(" "), /ran the whole suite instead of a shard/);
});
