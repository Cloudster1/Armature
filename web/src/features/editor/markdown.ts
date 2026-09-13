import type { Doc } from "@/api/issues";
import { HEADING_LEVELS, safeHref, type DocMark, type DocNode } from "./schema";

/**
 * Markdown is a way of typing the document, not a second format: these two
 * functions cover exactly the node set the schema names, and a document that
 * goes through both comes back the same.
 */

// ---------------------------------------------------------------- doc to md

export function docToMarkdown(doc: Doc | DocNode | null | undefined): string {
  if (!doc) return "";
  return blocksToMarkdown((doc.content ?? []) as DocNode[]).join("\n\n");
}

function blocksToMarkdown(blocks: DocNode[]): string[] {
  return blocks.map(blockToMarkdown);
}

function blockToMarkdown(block: DocNode): string {
  switch (block.type) {
    case "paragraph":
      return escapeLineStarts(inlineToMarkdown(block.content ?? []));
    case "heading": {
      // A heading is one line in markdown; a break inside it becomes a space.
      const level = Number(block.attrs?.level ?? 1);
      return `${"#".repeat(Math.min(Math.max(level, 1), 3))} ${inlineToMarkdown(block.content ?? []).replace(/\n/g, " ")}`;
    }
    case "blockquote":
      return blocksToMarkdown(block.content ?? [])
        .join("\n\n")
        .split("\n")
        .map((line) => (line ? `> ${line}` : ">"))
        .join("\n");
    case "codeBlock": {
      const language = typeof block.attrs?.language === "string" ? block.attrs.language : "";
      return "```" + language + "\n" + textOfCode(block) + "\n```";
    }
    case "bulletList":
    case "orderedList":
      return (block.content ?? [])
        .map((item, i) => {
          const marker = block.type === "orderedList" ? `${i + 1}. ` : "- ";
          const inner = blocksToMarkdown(item.content ?? []).join("\n\n").split("\n");
          return inner.map((line, k) => (k === 0 ? marker + line : line ? " ".repeat(marker.length) + line : "")).join("\n");
        })
        .join("\n");
    default:
      return escapeText(textOfNode(block));
  }
}

function textOfCode(block: DocNode): string {
  return (block.content ?? []).map((n) => n.text ?? "").join("");
}

function textOfNode(node: DocNode): string {
  if (node.type === "text") return node.text ?? "";
  return (node.content ?? []).map(textOfNode).join("");
}

/** A line that would read as a heading, a quote or a list item is escaped so it stays a paragraph. */
function escapeLineStarts(text: string): string {
  return text
    .split("\n")
    .map((line) => (/^(#{1,6}\s|>|[-*]\s|\d+\.\s|```)/.test(line) ? "\\" + line : line))
    .join("\n");
}

function inlineToMarkdown(nodes: DocNode[]): string {
  return nodes.map(inlineNodeToMarkdown).join("");
}

function inlineNodeToMarkdown(node: DocNode): string {
  switch (node.type) {
    case "hardBreak":
      return "\n";
    case "mention":
      return `@${String(node.attrs?.label ?? "")}`;
    case "text": {
      const marks = node.marks ?? [];
      const code = marks.find((m) => m.type === "code");
      if (code) return "`" + (node.text ?? "").replace(/`/g, "\\`") + "`";
      let out = escapeText(node.text ?? "");
      for (const mark of marks) out = wrap(out, mark);
      return out;
    }
    default:
      return escapeText(textOfNode(node));
  }
}

function wrap(text: string, mark: DocMark): string {
  switch (mark.type) {
    case "bold":
      return `**${text}**`;
    case "italic":
      return `*${text}*`;
    case "strike":
      return `~~${text}~~`;
    case "link": {
      const href = safeHref(mark.attrs?.href);
      return href ? `[${text}](${href})` : text;
    }
    default:
      return text;
  }
}

/** The characters that would be read as markup are escaped, so prose stays prose. */
function escapeText(text: string): string {
  return text.replace(/([\\*`~\[\]])/g, "\\$1");
}

// ---------------------------------------------------------------- md to doc

/** A blank text is no document at all, which the API stores as nothing. */
export function markdownToDoc(text: string): Doc | null {
  if (!text.trim()) return null;
  const blocks = parseBlocks(text.replace(/\r\n?/g, "\n").split("\n"));
  return { type: "doc", content: blocks };
}

const FENCE = /^```(\w*)\s*$/;
const HEADING = /^(#{1,3})\s+(.*)$/;
const QUOTE = /^>\s?(.*)$/;
const ITEM = /^(\s*)([-*]|\d+\.)\s+(.*)$/;

function parseBlocks(lines: string[]): DocNode[] {
  const out: DocNode[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i]!;
    if (!line.trim()) {
      i++;
      continue;
    }
    const fence = FENCE.exec(line);
    if (fence) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !FENCE.test(lines[i]!)) body.push(lines[i++]!);
      i++;
      const code: DocNode = { type: "codeBlock", attrs: { language: fence[1] ?? "" } };
      if (body.length > 0) code.content = [{ type: "text", text: body.join("\n") }];
      out.push(code);
      continue;
    }
    const heading = HEADING.exec(line);
    if (heading) {
      const level = heading[1]!.length as (typeof HEADING_LEVELS)[number];
      out.push({ type: "heading", attrs: { level }, content: parseInline(heading[2]!) });
      i++;
      continue;
    }
    if (QUOTE.test(line)) {
      const inner: string[] = [];
      while (i < lines.length && QUOTE.test(lines[i]!)) inner.push(QUOTE.exec(lines[i++]!)![1]!);
      out.push({ type: "blockquote", content: parseBlocks(inner) });
      continue;
    }
    const item = ITEM.exec(line);
    if (item) {
      const indent = item[1]!.length;
      const ordered = /\d/.test(item[2]!);
      const items: DocNode[] = [];
      while (i < lines.length) {
        const head = ITEM.exec(lines[i]!);
        if (!head || head[1]!.length !== indent || /\d/.test(head[2]!) !== ordered) break;
        const width = head[1]!.length + head[2]!.length + 1;
        const body = [head[3]!];
        i++;
        // Lines indented past the marker belong to the item.
        while (i < lines.length && (lines[i]!.startsWith(" ".repeat(width)) || (!lines[i]!.trim() && i + 1 < lines.length && lines[i + 1]!.startsWith(" ".repeat(width))))) {
          body.push(lines[i]!.slice(width));
          i++;
        }
        items.push({ type: "listItem", content: parseBlocks(body).length > 0 ? parseBlocks(body) : [{ type: "paragraph" }] });
      }
      out.push({ type: ordered ? "orderedList" : "bulletList", content: items });
      continue;
    }
    // A paragraph runs until a blank line or a block that reads as something else.
    const para: string[] = [];
    while (i < lines.length && lines[i]!.trim() && !FENCE.test(lines[i]!) && !HEADING.test(lines[i]!) && !QUOTE.test(lines[i]!) && !ITEM.test(lines[i]!)) {
      para.push(lines[i]!.startsWith("\\") && /^\\(#{1,6}\s|>|[-*]\s|\d+\.\s|```)/.test(lines[i]!) ? lines[i]!.slice(1) : lines[i]!);
      i++;
    }
    if (para.length === 0) {
      // A line that opened no block and is not prose, such as a lone marker.
      para.push(lines[i]!);
      i++;
    }
    const content: DocNode[] = [];
    para.forEach((text, k) => {
      if (k > 0) content.push({ type: "hardBreak" });
      content.push(...parseInline(text));
    });
    out.push(content.length > 0 ? { type: "paragraph", content } : { type: "paragraph" });
  }
  return out;
}

/** Inline markup, innermost first: code is never parsed inside, everything else nests. */
export function parseInline(text: string, marks: DocMark[] = []): DocNode[] {
  const out: DocNode[] = [];
  let buffer = "";
  const flush = () => {
    if (buffer) out.push(marks.length > 0 ? { type: "text", text: buffer, marks: [...marks] } : { type: "text", text: buffer });
    buffer = "";
  };
  let i = 0;
  while (i < text.length) {
    const ch = text[i]!;
    if (ch === "\\" && i + 1 < text.length) {
      buffer += text[i + 1];
      i += 2;
      continue;
    }
    if (ch === "`") {
      const end = findClose(text, i + 1, "`");
      if (end > i) {
        flush();
        out.push({ type: "text", text: text.slice(i + 1, end).replace(/\\`/g, "`"), marks: [...marks, { type: "code" }] });
        i = end + 1;
        continue;
      }
    }
    const delimited = [
      { open: "**", mark: "bold" },
      { open: "~~", mark: "strike" },
      { open: "*", mark: "italic" },
    ];
    let matched = false;
    for (const { open, mark } of delimited) {
      if (text.startsWith(open, i) && !marks.some((m) => m.type === mark)) {
        const end = findClose(text, i + open.length, open);
        if (end > i + open.length) {
          flush();
          out.push(...parseInline(text.slice(i + open.length, end), [...marks, { type: mark }]));
          i = end + open.length;
          matched = true;
          break;
        }
      }
    }
    if (matched) continue;
    if (ch === "[") {
      const close = findClose(text, i + 1, "]");
      if (close > i && text[close + 1] === "(") {
        const paren = text.indexOf(")", close + 2);
        const href = paren > close ? safeHref(text.slice(close + 2, paren)) : null;
        if (href) {
          flush();
          out.push(...parseInline(text.slice(i + 1, close), [...marks, { type: "link", attrs: { href } }]));
          i = paren + 1;
          continue;
        }
      }
    }
    buffer += ch;
    i++;
  }
  flush();
  return out;
}

/** The index of the next unescaped delimiter, or -1. */
function findClose(text: string, from: number, delimiter: string): number {
  for (let i = from; i < text.length; i++) {
    if (text[i] === "\\") {
      i++;
      continue;
    }
    if (text.startsWith(delimiter, i)) return i;
  }
  return -1;
}
