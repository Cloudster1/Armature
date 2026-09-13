import { useEffect, useState } from "react";
import { readStorage, writeStorage } from "@/features/shell/state";

export type EditorMode = "rich" | "markdown";

const KEY = "armature.editor";
const listeners = new Set<(mode: EditorMode) => void>();

function read(): EditorMode {
  return readStorage(KEY) === "markdown" ? "markdown" : "rich";
}

/**
 * Which face the editor shows, kept per browser like the sidebar and the
 * theme. Every editor on the page follows one switch, because a person
 * writing a comment under a description they just edited has one habit.
 */
export function useEditorMode(): [EditorMode, (mode: EditorMode) => void] {
  const [mode, setMode] = useState<EditorMode>(read);
  useEffect(() => {
    listeners.add(setMode);
    return () => {
      listeners.delete(setMode);
    };
  }, []);
  return [
    mode,
    (next) => {
      writeStorage(KEY, next);
      for (const listener of listeners) listener(next);
    },
  ];
}
