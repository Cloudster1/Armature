// How a run splits its scenarios across jobs, and how the jobs' runs add up.
// Kept free of the browser so it can be tested on its own.

/** The part of the run E2E_SHARD asks for, as { index, count }, or null for the whole run. */
export function parseShard(value) {
  const asked = (value ?? "").trim();
  if (!asked) return null;
  const match = /^(\d+)\/(\d+)$/.exec(asked);
  const index = match ? Number(match[1]) : 0;
  const count = match ? Number(match[2]) : 0;
  if (index < 1 || index > count) {
    throw new Error(`E2E_SHARD is ${JSON.stringify(asked)}; write it as i/n with i from 1 to n, such as 2/4.`);
  }
  return { index, count };
}

/** Which shard of the split a "i/n" names. */
const partOf = (shard) => Number(String(shard).split("/")[0]);

/** How the split names a scenario: by file, then name, so it does not move as files are reordered. */
const dealKey = (s) => `${s.file}\u0000${s.name}`;

/**
 * This shard's scenarios, dealt in turn from the list sorted by file and name,
 * so every job computes the same split and one file's scenarios spread out.
 */
export function shardOf(list, shard) {
  if (!shard) return list;
  const sorted = [...list].sort((a, b) => (dealKey(a) < dealKey(b) ? -1 : dealKey(a) > dealKey(b) ? 1 : 0));
  const mine = new Set(sorted.filter((_, i) => i % shard.count === shard.index - 1));
  return list.filter((s) => mine.has(s));
}

/**
 * The timings of a split run's jobs as one, and what is wrong with them: a job
 * missing, or a scenario run twice or not at all.
 */
export function mergeShards(runs) {
  const problems = [];
  const counts = new Set(runs.map((r) => r.shard?.split("/")[1]));
  const ofs = new Set(runs.map((r) => r.of));
  if (runs.length === 0) problems.push("No job left its timings; look at the browser jobs' logs for why.");
  if (runs.some((r) => !r.shard)) problems.push("A job ran the whole suite instead of a shard; give each job its SHARD.");
  if (counts.size > 1) problems.push(`The jobs split the suite in different ways (${[...counts].join(", ")}); give every job the same n in SHARD=i/n.`);
  if (ofs.size > 1) problems.push(`The jobs registered different scenarios (${[...ofs].join(", ")}); run every job from the same commit.`);
  const count = Number([...counts][0]);
  const seen = new Set(runs.map((r) => r.shard));
  for (let i = 1; counts.size === 1 && i <= count; i += 1) {
    if (!seen.has(`${i}/${count}`)) problems.push(`Shard ${i}/${count} left no timings; its job failed before the suite ended, or its artifact is missing.`);
  }
  const scenarios = runs.flatMap((r) => r.scenarios.map((s) => ({ ...s, shard: r.shard })));
  const byName = new Map();
  for (const s of scenarios) byName.set(s.name, [...(byName.get(s.name) ?? []), s.shard]);
  for (const [name, shards] of byName) {
    if (shards.length > 1) problems.push(`"${name}" ran in ${shards.join(" and ")}; a scenario belongs to one shard.`);
  }
  const of = [...ofs][0];
  if (ofs.size === 1 && problems.length === 0 && byName.size !== of) {
    problems.push(`The shards ran ${byName.size} scenarios, but the suite has ${of}; some were not run.`);
  }
  scenarios.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  const shards = runs
    .map((r) => ({ shard: r.shard, seconds: r.seconds, ran: r.scenarios.length, failed: r.scenarios.filter((s) => !s.ok).map((s) => s.name) }))
    .sort((a, b) => partOf(a.shard) - partOf(b.shard));
  return { problems, of, scenarios, shards };
}
