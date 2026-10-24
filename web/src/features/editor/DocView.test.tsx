import { readFileSync } from "node:fs";
import { resolve } from "node:path";
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
  // The look is the shared stylesheet's, as in Stator and Exciter; no block carries a look of its own.
  it("draws every block kind under .doc-content, styled by its rules alone", () => {
    const every: Doc = {
      type: "doc",
      content: [
        ...doc.content!,
        { type: "heading", attrs: { level: 1 }, content: [{ type: "text", text: "Top" }] },
        { type: "heading", attrs: { level: 3 }, content: [{ type: "text", text: "Low" }] },
        { type: "orderedList", content: [{ type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "first" }] }] }] },
        { type: "blockquote", content: [{ type: "paragraph", content: [{ type: "text", text: "quoted", marks: [{ type: "code" }] }] }] },
      ],
    };
    const { container } = render(<DocView doc={every} />);
    const root = container.querySelector("[data-doc]")!;
    expect(root.classList.contains("doc-content")).toBe(true);
    for (const tag of ["h2", "h3", "h4", "ul", "ol", "li", "blockquote", "pre", "code", "a", "[data-mention]"]) {
      const el = root.querySelector(tag);
      expect(el, tag).not.toBeNull();
      expect(el!.getAttribute("class"), `${tag} has no classes of its own`).toBeNull();
    }

    const css = readFileSync(resolve(process.cwd(), "src/styles/index.css"), "utf8");
    for (const selector of [".doc-content :is(h1, h2, h3, h4)", ".doc-content h2", ".doc-content h3", ".doc-content ul", ".doc-content ol", ".doc-content blockquote", ".doc-content a", ".doc-content :not(pre) > code", ".doc-content pre", ".doc-content [data-mention]"]) {
      expect(css, selector).toContain(`${selector} {`);
    }
  });

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
