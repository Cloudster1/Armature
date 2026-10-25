import { useState } from "react";
import { useEditorState, type Editor } from "@tiptap/react";
import { Button, Field, IconButton, Popover } from "@/components/ui";
import { Icon } from "@/components/icons";
import { safeHref } from "./schema";
import { t } from "@/i18n";

/**
 * The buttons over the rich editor: one per construct the schema names, and
 * nothing the document could not hold. Markdown shortcuts do the same by
 * typing, so the bar is for people who do not know them.
 */
export function EditorToolbar({ editor }: { editor: Editor }) {
  const state = useEditorState({
    editor,
    selector: ({ editor: e }) => ({
      bold: e.isActive("bold"),
      italic: e.isActive("italic"),
      code: e.isActive("code"),
      strike: e.isActive("strike"),
      heading: e.isActive("heading", { level: 2 }),
      bullet: e.isActive("bulletList"),
      ordered: e.isActive("orderedList"),
      quote: e.isActive("blockquote"),
      codeBlock: e.isActive("codeBlock"),
      link: e.isActive("link"),
      href: (e.getAttributes("link").href as string | undefined) ?? "",
    }),
  });
  const [linking, setLinking] = useState(false);
  const [href, setHref] = useState("");
  const chain = () => editor.chain().focus();

  const buttons: Array<{ action: string; label: string; icon: React.ReactNode; on: boolean; run: () => void }> = [
    { action: "bold", label: t.kit.editor.bold, icon: <Icon.Bold />, on: state.bold, run: () => chain().toggleBold().run() },
    { action: "italic", label: t.kit.editor.italic, icon: <Icon.Italic />, on: state.italic, run: () => chain().toggleItalic().run() },
    { action: "code", label: t.kit.editor.code, icon: <Icon.Code />, on: state.code, run: () => chain().toggleCode().run() },
    { action: "heading", label: t.kit.editor.heading, icon: <Icon.Heading />, on: state.heading, run: () => chain().toggleHeading({ level: 2 }).run() },
    { action: "bullet-list", label: t.kit.editor.bulletList, icon: <Icon.Lines />, on: state.bullet, run: () => chain().toggleBulletList().run() },
    { action: "ordered-list", label: t.kit.editor.numberedList, icon: <Icon.OrderedList />, on: state.ordered, run: () => chain().toggleOrderedList().run() },
    { action: "quote", label: t.kit.editor.quote, icon: <Icon.Quote />, on: state.quote, run: () => chain().toggleBlockquote().run() },
    { action: "code-block", label: t.kit.editor.codeBlock, icon: <Icon.Component />, on: state.codeBlock, run: () => chain().toggleCodeBlock().run() },
  ];

  return (
    <div className="flex flex-wrap items-center gap-0.5" role="toolbar" aria-label={t.kit.editor.formatting}>
      {buttons.map((b) => (
        <IconButton
          key={b.action}
          icon={b.icon}
          label={b.label}
          size="sm"
          variant="ghost"
          aria-pressed={b.on}
          data-editor-action={b.action}
          onMouseDown={(e) => e.preventDefault()}
          onClick={b.run}
        />
      ))}
      <Popover
        open={linking}
        onClose={() => setLinking(false)}
        label={t.kit.editor.link}
        trigger={
          <IconButton
            icon={<Icon.Link />}
            label={t.kit.editor.link}
            size="sm"
            variant="ghost"
            aria-pressed={state.link}
            aria-expanded={linking}
            data-editor-action="link"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              setHref(state.href);
              setLinking((open) => !open);
            }}
          />
        }
      >
        <form
          className="flex items-end gap-2 p-1"
          onSubmit={(event) => {
            event.preventDefault();
            const safe = safeHref(href);
            if (!safe) return;
            chain().extendMarkRange("link").setLink({ href: safe }).run();
            setLinking(false);
          }}
        >
          <Field
            label={t.kit.editor.linkAddress}
            id="field-link-address"
            value={href}
            onChange={(e) => setHref(e.target.value)}
            placeholder="https://"
            className="w-64"
          />
          <Button type="submit" size="sm" disabled={!safeHref(href)}>
            {t.kit.editor.setLink}
          </Button>
          {state.link && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                chain().extendMarkRange("link").unsetLink().run();
                setLinking(false);
              }}
            >
              {t.kit.editor.removeLink}
            </Button>
          )}
        </form>
      </Popover>
    </div>
  );
}
