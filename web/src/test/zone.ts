import { afterEach, vi } from "vitest";

/**
 * The browser's zone and clock for one test. Node rereads TZ whenever it is set,
 * so a zone other than the profile's shows which one a date was worked out in.
 */
export function atInstantIn(browserZone: string, instant: string): void {
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
