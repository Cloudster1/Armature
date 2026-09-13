import type { Doc } from "@/api/issues";

/**
 * The document the editor, the renderer, the markdown converters and the
 * server all agree on. Nothing outside this set reaches the database, so a
 * document reads back whole on every screen.
 */
export const NODE_TYPES = ["doc", "paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "codeBlock", "hardBreak", "text", "mention"] as const;
export const MARK_TYPES = ["bold", "italic", "code", "strike", "link"] as const;
export const HEADING_LEVELS = [1, 2, 3] as const;

export type NodeType = (typeof NODE_TYPES)[number];
export type MarkType = (typeof MARK_TYPES)[number];

export interface DocMark {
  type: string;
  attrs?: Record<string, unknown>;
}

export interface DocNode {
  type: string;
  text?: string;
  attrs?: Record<string, unknown>;
  marks?: DocMark[];
  content?: DocNode[];
}

export interface MentionAttrs {
  id: string;
  label: string;
}

/** Somebody the editor can name with an at sign. */
export interface Mentionable {
  id: string;
  name: string;
  email?: string;
}

/** A document that says nothing: no blocks, or only paragraphs with nothing in them. */
export function isEmptyDoc(doc: Doc | DocNode | null | undefined): boolean {
  if (!doc) return true;
  const content = (doc.content ?? []) as DocNode[];
  return content.every((block) => block.type === "paragraph" && !(block.content && block.content.length > 0));
}

/** A web or a mail address, and nothing that runs; anything else is shown as text. */
export function safeHref(href: unknown): string | null {
  if (typeof href !== "string") return null;
  const lower = href.trim().toLowerCase();
  return lower.startsWith("http://") || lower.startsWith("https://") || lower.startsWith("mailto:") ? href.trim() : null;
}

/** The text of a node and everything under it, for previews and for the unknown. */
export function textOf(node: DocNode): string {
  if (node.type === "text") return node.text ?? "";
  if (node.type === "mention") return `@${String(node.attrs?.label ?? "")}`;
  if (node.type === "hardBreak") return "\n";
  return (node.content ?? []).map(textOf).join("");
}
