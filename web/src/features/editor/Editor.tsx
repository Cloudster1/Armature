import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { EditorContent, useEditor } from "@tiptap/react";
import { Extension, type JSONContent } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Mention from "@tiptap/extension-mention";
import { Placeholder } from "@tiptap/extensions";
import type { SuggestionProps } from "@tiptap/suggestion";
import type { Doc } from "@/api/issues";
import { MentionTextarea, Segmented, Textarea, cx, controlClass, mentionMatches } from "@/components/ui";
import { MENTION_MAX_SUGGESTIONS } from "@/config";
import { docToMarkdown, markdownToDoc } from "./markdown";
import { MentionList } from "./MentionList";
import { HEADING_LEVELS, isEmptyDoc, type DocNode, type Mentionable } from "./schema";
import { EditorToolbar } from "./Toolbar";
import { useEditorMode } from "./useEditorMode";

/** What a form may do to the editor from outside: put words in, or empty it. */
export interface EditorHandle {
  insertMarkdown: (text: string) => void;
  clear: () => void;
}

export interface EditorProps {
  /** The id the suite types into: on the textarea or on the editable div. */
  id: string;
  value: Doc | null;
  /** Null when the document says nothing, so a blank description clears. */
  onChange: (doc: Doc | null) => void;
  people?: Mentionable[];
  placeholder?: string;
  autoFocus?: boolean;
  rows?: number;
  /** Ctrl or Cmd with Enter submits the surrounding form. */
  onSubmit?: () => void;
  "aria-label"?: string;
  handle?: (handle: EditorHandle) => void;
}

interface Suggesting {
  items: Mentionable[];
  rect: DOMRect | null;
  command: (attrs: { id: string; label: string }) => void;
}

const LINE_HEIGHT_PX = 22;

const emptyDoc: Doc = { type: "doc", content: [{ type: "paragraph" }] };

/**
 * One editor with two faces over one document: the rich face edits it in
 * place, the markdown face edits the text it converts to. Switching converts
 * the draft, so nothing typed is lost, and the choice is kept per browser.
 */
export function Editor({ id, value, onChange, people, placeholder, autoFocus = false, rows = 4, onSubmit, "aria-label": ariaLabel = "Description", handle }: EditorProps) {
  const [mode, setMode] = useEditorMode();
  const [text, setText] = useState(() => docToMarkdown(value));
  const [suggesting, setSuggesting] = useState<Suggesting | null>(null);
  const [active, setActive] = useState(0);
  const suggest = useRef<Suggesting | null>(null);
  const activeRef = useRef(0);
  const submitRef = useRef(onSubmit);
  submitRef.current = onSubmit;
  const peopleRef = useRef(people ?? []);
  peopleRef.current = people ?? [];

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ underline: false, horizontalRule: false, heading: { levels: [...HEADING_LEVELS] }, link: { openOnClick: false, autolink: false } }),
      Placeholder.configure({ placeholder: placeholder ?? "" }),
      Mention.configure({
        renderText: ({ node }) => `@${String(node.attrs.label ?? "")}`,
        HTMLAttributes: { "data-mention": "" },
        suggestion: {
          char: "@",
          items: ({ query }) => mentionMatches(peopleRef.current, query).slice(0, MENTION_MAX_SUGGESTIONS),
          render: () => ({
            onStart: (props: SuggestionProps<Mentionable>) => show(props),
            onUpdate: (props: SuggestionProps<Mentionable>) => show(props),
            onKeyDown: ({ event }) => keyInList(event),
            onExit: () => {
              suggest.current = null;
              setSuggesting(null);
            },
          }),
        },
      }),
      Extension.create({
        name: "submitOnModEnter",
        addKeyboardShortcuts() {
          return {
            "Mod-Enter": () => {
              submitRef.current?.();
              return true;
            },
          };
        },
      }),
    ],
    content: (value ?? emptyDoc) as JSONContent,
    autofocus: autoFocus && mode === "rich" ? "end" : false,
    editorProps: {
      attributes: {
        id,
        role: "textbox",
        "aria-multiline": "true",
        "aria-label": ariaLabel,
        "data-editor": "rich",
        class: cx(controlClass, "h-auto min-h-0 py-2 leading-normal outline-none [&_p]:my-0 [&_h2]:text-lg [&_h2]:font-semibold [&_h3]:font-semibold [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:list-decimal [&_ol]:pl-5 [&_blockquote]:border-l-2 [&_blockquote]:border-border-strong [&_blockquote]:pl-3 [&_pre]:rounded-control [&_pre]:bg-surface-sunken [&_pre]:px-3 [&_pre]:py-2 [&_pre]:font-mono [&_pre]:text-sm [&_code]:font-mono [&_[data-mention]]:text-accent [&_[data-mention]]:font-medium [&_p.is-editor-empty:first-child::before]:pointer-events-none [&_p.is-editor-empty:first-child::before]:float-left [&_p.is-editor-empty:first-child::before]:h-0 [&_p.is-editor-empty:first-child::before]:text-ink-subtle [&_p.is-editor-empty:first-child::before]:content-[attr(data-placeholder)]"),
        style: `min-height: ${rows * LINE_HEIGHT_PX}px`,
      },
    },
    onUpdate: ({ editor: e }) => {
      const json = e.getJSON() as Doc;
      onChange(isEmptyDoc(json as unknown as DocNode) ? null : json);
    },
  });

  function show(props: SuggestionProps<Mentionable>) {
    const next: Suggesting = { items: props.items, rect: props.clientRect?.() ?? null, command: (attrs) => props.command(attrs) };
    suggest.current = next;
    activeRef.current = 0;
    setActive(0);
    setSuggesting(next);
  }

  function keyInList(event: globalThis.KeyboardEvent): boolean {
    const current = suggest.current;
    if (!current || current.items.length === 0) return false;
    const move = (delta: number) => {
      activeRef.current = (activeRef.current + delta + current.items.length) % current.items.length;
      setActive(activeRef.current);
    };
    switch (event.key) {
      case "ArrowDown":
        move(1);
        return true;
      case "ArrowUp":
        move(-1);
        return true;
      case "Enter":
      case "Tab": {
        const person = current.items[activeRef.current];
        if (person) current.command({ id: person.id, label: person.name });
        return true;
      }
      case "Escape":
        suggest.current = null;
        setSuggesting(null);
        return true;
    }
    return false;
  }

  // The form outside reaches in through a handle rather than a ref, so the
  // kit's rule of plain props holds.
  useEffect(() => {
    if (!handle) return;
    handle({
      insertMarkdown: (markdown) => {
        if (mode === "markdown") {
          setText((t) => {
            const next = t.trim() ? `${t}\n${markdown}` : markdown;
            onChange(markdownToDoc(next));
            return next;
          });
          return;
        }
        const doc = markdownToDoc(markdown);
        if (doc && editor) {
          editor.chain().focus("end").insertContent(doc.content as JSONContent[]).run();
        }
      },
      clear: () => {
        setText("");
        editor?.commands.clearContent(true);
        onChange(null);
      },
    });
  }, [handle, mode, editor, onChange]);

  function switchTo(next: "rich" | "markdown") {
    if (next === mode) return;
    if (next === "markdown") {
      setText(editor ? docToMarkdown(editor.getJSON() as Doc) : docToMarkdown(value));
    } else if (editor) {
      const doc = markdownToDoc(text);
      editor.commands.setContent((doc ?? emptyDoc) as JSONContent);
      onChange(doc);
    }
    setMode(next);
  }

  function onTextareaKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && (event.ctrlKey || event.metaKey) && onSubmit) {
      event.preventDefault();
      onSubmit();
    }
  }

  // The markdown face grows with its lines, so nothing typed is out of sight.
  const lines = Math.max(rows, text.split("\n").length + 1);
  const textarea = people ? (
    <MentionTextarea
      id={id}
      value={text}
      onValueChange={(next) => {
        setText(next);
        onChange(markdownToDoc(next));
      }}
      people={people}
      rows={lines}
      placeholder={placeholder}
      autoFocus={autoFocus}
      aria-label={ariaLabel}
      onKeyDown={onTextareaKeyDown}
      data-editor="markdown"
      className="font-mono text-sm"
    />
  ) : (
    <Textarea
      id={id}
      value={text}
      onChange={(event) => {
        setText(event.target.value);
        onChange(markdownToDoc(event.target.value));
      }}
      rows={lines}
      placeholder={placeholder}
      autoFocus={autoFocus}
      aria-label={ariaLabel}
      onKeyDown={onTextareaKeyDown}
      data-editor="markdown"
      className="font-mono text-sm"
    />
  );

  return (
    <div className="space-y-1.5" data-editor-frame>
      <div className="flex flex-wrap items-center justify-between gap-2">
        {mode === "rich" && editor ? <EditorToolbar editor={editor} /> : <span className="text-xs text-ink-subtle">Markdown: **bold**, *italic*, # heading, - list, &gt; quote, ``` code</span>}
        <Segmented
          label="Editor mode"
          value={mode}
          size="sm"
          onChange={(next) => switchTo(next as "rich" | "markdown")}
          options={[
            { value: "rich", label: "Rich", attrs: { "data-editor-mode": "rich" } },
            { value: "markdown", label: "Markdown", attrs: { "data-editor-mode": "markdown" } },
          ]}
        />
      </div>
      {mode === "rich" ? (
        <div className="relative">
          <EditorContent editor={editor} />
          {suggesting && (
            <MentionList
              items={suggesting.items}
              active={active}
              rect={suggesting.rect}
              onHover={(i) => {
                activeRef.current = i;
                setActive(i);
              }}
              onPick={(person) => suggesting.command({ id: person.id, label: person.name })}
            />
          )}
        </div>
      ) : (
        textarea
      )}
    </div>
  );
}
