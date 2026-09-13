import { Fragment, type ReactNode } from "react";
import type { Doc } from "@/api/issues";
import { cx } from "@/components/ui";
import { safeHref, textOf, type DocNode } from "./schema";

/**
 * A document drawn as elements, never as HTML: every node becomes the React
 * element that means it, so nothing a person typed is ever parsed as markup
 * and an unsafe link is shown as text.
 */
export function DocView({ doc, className, size = "base" }: { doc: Doc | DocNode | null | undefined; className?: string; size?: "sm" | "base" }) {
  if (!doc) return null;
  return (
    <div className={cx("prose-doc space-y-2", size === "sm" ? "text-sm" : "text-base", "text-ink", className)} data-doc>
      {((doc.content ?? []) as DocNode[]).map((block, i) => (
        <Block key={i} node={block} />
      ))}
    </div>
  );
}

function Block({ node }: { node: DocNode }) {
  switch (node.type) {
    case "paragraph":
      return <p className="whitespace-pre-wrap">{inline(node.content)}</p>;
    case "heading": {
      const level = Number(node.attrs?.level ?? 1);
      // The page owns its h1, so a document's levels start one down.
      if (level <= 1) return <h2 className="text-lg font-semibold">{inline(node.content)}</h2>;
      if (level === 2) return <h3 className="text-md font-semibold">{inline(node.content)}</h3>;
      return <h4 className="font-semibold">{inline(node.content)}</h4>;
    }
    case "bulletList":
      return <ul className="list-disc space-y-1 pl-5">{items(node.content)}</ul>;
    case "orderedList":
      return <ol className="list-decimal space-y-1 pl-5">{items(node.content)}</ol>;
    case "blockquote":
      return (
        <blockquote className="border-l-2 border-border-strong pl-3 text-ink-muted">
          {(node.content ?? []).map((child, i) => (
            <Block key={i} node={child} />
          ))}
        </blockquote>
      );
    case "codeBlock":
      return (
        <pre className="overflow-x-auto rounded-control bg-surface-sunken px-3 py-2 font-mono text-sm">
          <code data-language={typeof node.attrs?.language === "string" ? node.attrs.language : undefined}>{(node.content ?? []).map((n) => n.text ?? "").join("")}</code>
        </pre>
      );
    default:
      return <span className="whitespace-pre-wrap">{textOf(node)}</span>;
  }
}

function items(nodes: DocNode[] | undefined): ReactNode {
  return (nodes ?? []).map((item, i) => (
    <li key={i} className="space-y-1">
      {(item.content ?? []).map((child, k) =>
        child.type === "paragraph" ? <span key={k} className="block whitespace-pre-wrap">{inline(child.content)}</span> : <Block key={k} node={child} />,
      )}
    </li>
  ));
}

function inline(nodes: DocNode[] | undefined): ReactNode {
  return (nodes ?? []).map((node, i) => <Fragment key={i}>{inlineNode(node)}</Fragment>);
}

function inlineNode(node: DocNode): ReactNode {
  switch (node.type) {
    case "text":
      return marked(node.text ?? "", node.marks ?? []);
    case "hardBreak":
      return <br />;
    case "mention":
      return (
        <span className="rounded-control bg-accent-subtle px-1 font-medium text-accent" data-mention={String(node.attrs?.id ?? "")}>
          @{String(node.attrs?.label ?? "")}
        </span>
      );
    default:
      return textOf(node);
  }
}

/** Marks nest in the order they are listed; a link that is not a web or mail address is plain text. */
function marked(text: string, marks: DocNode["marks"]): ReactNode {
  let out: ReactNode = text;
  for (const mark of marks ?? []) {
    switch (mark.type) {
      case "bold":
        out = <strong>{out}</strong>;
        break;
      case "italic":
        out = <em>{out}</em>;
        break;
      case "code":
        out = <code className="rounded-control bg-surface-sunken px-1 font-mono text-[0.9em]">{out}</code>;
        break;
      case "strike":
        out = <s>{out}</s>;
        break;
      case "link": {
        const href = safeHref(mark.attrs?.href);
        if (href) out = <a href={href} rel="noopener noreferrer" target="_blank" className="text-accent underline">{out}</a>;
        break;
      }
    }
  }
  return out;
}
