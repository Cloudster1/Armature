import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import type { Doc } from "@/api/issues";
import { DocView } from "./DocView";

const doc: Doc = {
  type: "doc",
  content: [
    { type: "heading", attrs: { level: 2 }, content: [{ type: "text", text: "Plan" }] },
    { type: "paragraph", content: [{ type: "text", text: "bold", marks: [{ type: "bold" }] }, { type: "text", text: " and " }, { type: "mention", attrs: { id: "u1", label: "Ada" } }, { type: "hardBreak" }, { type: "text", text: "next line" }] },
    { type: "bulletList", content: [{ type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "item" }] }] }] },
    { type: "codeBlock", attrs: { language: "go" }, content: [{ type: "text", text: "x := 1" }] },
    { type: "paragraph", content: [{ type: "text", text: "safe", marks: [{ type: "link", attrs: { href: "https://example.test" } }] }, { type: "text", text: "unsafe", marks: [{ type: "link", attrs: { href: "javascript:alert(1)" } }] }] },
    { type: "table", content: [{ type: "text", text: "unknown" }] },
  ],
};

describe("DocView", () => {
  it("draws each node as the element that means it", () => {
    const { container } = render(<DocView doc={doc} />);
    expect(container.querySelector("h3")?.textContent).toBe("Plan");
    expect(container.querySelector("strong")?.textContent).toBe("bold");
    expect(container.querySelector('[data-mention="u1"]')?.textContent).toBe("@Ada");
    expect(container.querySelector("br")).not.toBeNull();
    expect(container.querySelector("ul li")?.textContent).toBe("item");
    expect(container.querySelector('code[data-language="go"]')?.textContent).toBe("x := 1");
    expect(container.textContent).toContain("unknown");
  });

  // The one rule that keeps a document from being an injection: no HTML, ever.
  it("never turns an unsafe link into an anchor and never sets HTML", () => {
    const { container } = render(<DocView doc={doc} />);
    const anchors = Array.from(container.querySelectorAll("a"));
    expect(anchors).toHaveLength(1);
    expect(anchors[0]?.getAttribute("href")).toBe("https://example.test");
    expect(anchors[0]?.getAttribute("rel")).toBe("noopener noreferrer");
    expect(container.textContent).toContain("unsafe");
    const scripted: Doc = { type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "<img src=x onerror=alert(1)>" }] }] };
    const { container: other } = render(<DocView doc={scripted} />);
    expect(other.querySelector("img")).toBeNull();
    expect(other.textContent).toContain("<img src=x onerror=alert(1)>");
  });
});
