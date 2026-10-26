// Adds up the timings a split browser run's jobs left, for CI's summary, and
// fails when a job is missing or a scenario ran twice or not at all.
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { mergeShards } from "./shard.mjs";

/** How many of the slowest scenarios the summary lists. */
const SLOWEST_SHOWN = 10;

const dir = process.argv[2];
if (!dir) {
  process.stderr.write("Name the directory the jobs' artifacts were downloaded to: node report.mjs <dir>\n");
  process.exit(1);
}

// One directory per job, as the artifacts download, each with the timings its run wrote.
const runs = readdirSync(dir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && existsSync(join(dir, entry.name, "timings.json")))
  .map((entry) => JSON.parse(readFileSync(join(dir, entry.name, "timings.json"), "utf8")));

const { problems, of, scenarios, shards } = mergeShards(runs);
writeFileSync(join(dir, "timings.json"), `${JSON.stringify({ of, shards, scenarios }, null, 2)}\n`);

const failed = shards.flatMap((s) => s.failed.map((name) => `- ${name} (shard ${s.shard})`));
const slowest = [...scenarios].sort((a, b) => b.ms - a.ms).slice(0, SLOWEST_SHOWN);
const lines = [
  "## Browser suite",
  "",
  `${scenarios.length} of ${of ?? "?"} scenarios ran across ${shards.length} shards; ${failed.length} failed.`,
  "",
  "| Shard | Scenarios | Seconds | Failed |",
  "|---|---|---|---|",
  ...shards.map((s) => `| ${s.shard} | ${s.ran} | ${s.seconds} | ${s.failed.length} |`),
  "",
  ...(problems.length ? ["### What is wrong with the split", "", ...problems.map((p) => `- ${p}`), ""] : []),
  ...(failed.length ? ["### Failed", "", ...failed, ""] : []),
  "### Slowest",
  "",
  "| ms | Scenario | File |",
  "|---|---|---|",
  ...slowest.map((s) => `| ${s.ms} | ${s.name} | ${s.file} |`),
  "",
];
process.stdout.write(lines.join("\n"));
for (const problem of problems) process.stderr.write(`${problem}\n`);
process.exit(problems.length ? 1 : 0);
