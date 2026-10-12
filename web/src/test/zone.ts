import { isMainThread } from "node:worker_threads";
import { afterEach, vi } from "vitest";

/**
 * The browser's zone and clock for one test. Node rereads TZ whenever it is set,
 * so a zone other than the profile's shows which one a date was worked out in.
 */
export function atInstantIn(browserZone: string, instant: string): void {
  // A worker thread ignores TZ, so the test would pass without ever leaving UTC.
  if (!isMainThread) {
    throw new Error("This test changes the time zone, which a worker thread cannot: name its file *.zone.test.ts(x) so it runs in a process of its own.");
  }
  process.env.TZ = browserZone;
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date(instant));
}

const startingZone = process.env.TZ;

afterEach(() => {
  vi.useRealTimers();
  if (startingZone === undefined) delete process.env.TZ;
  else process.env.TZ = startingZone;
});
