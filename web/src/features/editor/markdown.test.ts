import { describe, expect, it } from "vitest";
import type { Doc } from "@/api/issues";
import { docToMarkdown, markdownToDoc, parseInline } from "./markdown";
import type { DocNode } from "./schema";

const p = (...content: DocNode[]): DocNode => (content.length > 0 ? { type: "paragraph", content } : { type: "paragraph" });
const t = (text: string, ...marks: string[]): DocNode => (marks.length > 0 ? { type: "text", text, marks: marks.map((type) => ({ type })) } : { type: "text", text });
const doc = (...content: DocNode[]): Doc => ({ type: "doc", content });

describe("markdown and the document", () => {
  it("writes every construct the schema names", () => {
    const rich = doc(
      { type: "heading", attrs: { level: 2 }, content: [t("Plan")] },
      p(t("Ask ", "bold"), { type: "mention", attrs: { id: "u1", label: "Ada Lovelace" } }, t(" first, "), t("softly", "italic"), t(" and "), t("x = 1", "code"), t(", or "), t("not", "strike"), t(".")),
      { type: "bulletList", content: [{ type: "listItem", content: [p(t("one"))] }, { type: "listItem", content: [p(t("two"), { type: "hardBreak" }, t("more"))] }] },
      { type: "orderedList", content: [{ type: "listItem", content: [p(t("first"))] }] },
      { type: "blockquote", content: [p(t("quoted"))] },
      { type: "codeBlock", attrs: { language: "go" }, content: [t("x := 1\ny := 2")] },
      p({ type: "text", text: "site", marks: [{ type: "link", attrs: { href: "https://example.test" } }] }),
    );
    expect(docToMarkdown(rich)).toBe(
      [
        "## Plan",
        "",
        "**Ask **@Ada Lovelace first, *softly* and `x = 1`, or ~~not~~.",
        "",
        "- one",
        "- two",
        "  more",
        "",
        "1. first",
        "",
        "> quoted",
        "",
        "```go",
        "x := 1",
        "y := 2",
        "```",
        "",
        "[site](https://example.test)",
      ].join("\n"),
    );
  });

  it("reads them back, with a mention as the text it was typed as", () => {
    const back = markdownToDoc("## Plan\n\n**Ask** @Ada, *softly* and `x = 1`.\n\n- one\n- two\n  more\n\n1. first\n\n> quoted\n\n```go\nx := 1\n```\n\n[site](https://example.test)");
    expect(back).toEqual(
      doc(
        { type: "heading", attrs: { level: 2 }, content: [t("Plan")] },
        p(t("Ask", "bold"), t(" @Ada, "), t("softly", "italic"), t(" and "), t("x = 1", "code"), t(".")),
        { type: "bulletList", content: [{ type: "listItem", content: [p(t("one"))] }, { type: "listItem", content: [p(t("two"), { type: "hardBreak" }, t("more"))] }] },
        { type: "orderedList", content: [{ type: "listItem", content: [p(t("first"))] }] },
        { type: "blockquote", content: [p(t("quoted"))] },
        { type: "codeBlock", attrs: { language: "go" }, content: [t("x := 1")] },
        p({ type: "text", text: "site", marks: [{ type: "link", attrs: { href: "https://example.test" } }] }),
      ),
    );
  });

  // The stored shape of anything typed into a plain box: one paragraph with
  // newlines in it. It must come back with its lines.
  it("keeps the lines of a plain paragraph and treats blank text as no document", () => {
    expect(markdownToDoc("two\nlines")).toEqual(doc(p(t("two"), { type: "hardBreak" }, t("lines"))));
    expect(markdownToDoc("   \n ")).toBeNull();
    expect(docToMarkdown(null)).toBe("");
  });

  it("never turns text into markup it did not mean, and never into HTML", () => {
    const literal = doc(p(t("2 * 3 = 6, see [docs] and `tick`")), p(t("- not a list")), p(t("# not a heading")));
    const md = docToMarkdown(literal);
    expect(markdownToDoc(md)).toEqual(literal);
    expect(markdownToDoc("<script>alert(1)</script>")).toEqual(doc(p(t("<script>alert(1)</script>"))));
    expect(parseInline("[x](javascript:alert(1))")).toEqual([t("[x](javascript:alert(1))")]);
  });

  // A seeded walk over the node set: whatever the editor can hold survives a
  // trip through the textarea.
  it("round-trips random documents", () => {
    let seed = 7;
    const rand = (n: number) => {
      seed = (seed * 1103515245 + 12345) % 2147483648;
      return seed % n;
    };
    const words = ["alpha", "beta", "x*y", "a`b", "[q]", "one two", "~tilde~", "back\\slash", "3. three"];
    const marks = ["bold", "italic", "code", "strike"];
    const inline = (breaks = true): DocNode[] => {
      const out: DocNode[] = [];
      const n = 1 + rand(3);
      for (let i = 0; i < n; i++) {
        const mark = rand(3) === 0 ? marks[rand(marks.length)]! : null;
        const text = words[rand(words.length)]!;
        out.push(mark ? t(text, mark) : t(text));
        if (breaks && rand(4) === 0) out.push({ type: "hardBreak" }, t(words[rand(words.length)]!));
      }
      return out;
    };
    const block = (depth: number): DocNode => {
      switch (depth > 1 ? rand(2) : rand(5)) {
        case 0:
          return p(...inline());
        case 1:
          // A heading is one line in markdown, so it carries no breaks here.
          return { type: "heading", attrs: { level: 1 + rand(3) }, content: inline(false) };
        case 2:
          return { type: rand(2) ? "bulletList" : "orderedList", content: Array.from({ length: 1 + rand(2) }, () => ({ type: "listItem", content: [block(depth + 1)] })) };
        case 3:
          return { type: "blockquote", content: [block(depth + 1)] };
        default:
          return { type: "codeBlock", attrs: { language: "" }, content: [t(words[rand(words.length)]!)] };
      }
    };
    for (let round = 0; round < 60; round++) {
      const original = doc(...Array.from({ length: 1 + rand(3) }, () => block(0)));
      const md = docToMarkdown(original);
      expect(normalise(markdownToDoc(md) as Doc), md).toEqual(normalise(original));
    }
  });
});

/** Adjacent text nodes with the same marks are one node either way. */
function normalise(d: Doc): Doc {
  const merge = (nodes: DocNode[]): DocNode[] => {
    const out: DocNode[] = [];
    for (const node of nodes) {
      const child: DocNode = node.content ? { ...node, content: merge(node.content) } : { ...node };
      if (child.type === "text" && child.marks) child.marks = [...child.marks].sort((a, b) => a.type.localeCompare(b.type));
      const last = out[out.length - 1];
      if (last && last.type === "text" && child.type === "text" && JSON.stringify(last.marks ?? []) === JSON.stringify(child.marks ?? [])) {
        last.text = (last.text ?? "") + (child.text ?? "");
      } else {
        out.push(child);
      }
    }
    return out;
  };
  return { type: "doc", content: merge(d.content as DocNode[]) };
}
