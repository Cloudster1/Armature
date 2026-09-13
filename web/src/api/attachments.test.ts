import { describe, expect, it } from "vitest";
import { canPreview, formatSize } from "./attachments";

describe("formatSize", () => {
  it("reads like a person would say it", () => {
    expect(formatSize(0)).toBe("0 B");
    expect(formatSize(512)).toBe("512 B");
    expect(formatSize(12_288)).toBe("12 KB");
    expect(formatSize(3.4 * 1024 * 1024)).toBe("3.4 MB");
    expect(formatSize(25 * 1024 * 1024)).toBe("25 MB");
  });
});

describe("canPreview", () => {
  it("previews what a browser can show safely", () => {
    expect(canPreview("image/png")).toBe(true);
    expect(canPreview("application/pdf")).toBe(true);
    expect(canPreview("text/html")).toBe(false);
    expect(canPreview("application/zip")).toBe(false);
  });
});
