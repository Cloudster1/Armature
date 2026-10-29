import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { canAdminister, useAccess } from "@/api/access";
import { createPortal } from "react-dom";
import { useNavigate } from "@tanstack/react-router";
import { useIssues } from "@/api/issues";
import { useProject, useProjectSearch } from "@/api/projects";
import { answer, looksLikeQuestion } from "@/features/guide/match";
import { CATALOGUE, type GuideEntry } from "@/features/guide/catalogue";
import { useAsk, useAssistantStatus, type AssistantAnswer, type AssistantProposal, type Place } from "@/api/assistant";
import { AskAnswer } from "./AskAnswer";
import { Icon, type IconName } from "@/components/icons";
import { Input, cx, modalBackdropStyle, modalPanelStyle } from "@/components/ui";
import { PALETTE_RESULTS } from "@/config";
import { applyTheme, readTheme, type Theme } from "@/lib/theme";
import { pagesFor, projectWorkPages } from "@/features/shell/Sidebar";
import { settingsPagesFor, setupAddress } from "@/features/projects/settingsTabs";
import { adminEntry, adminFor, personalPlaces, placeKey, workPlaces, type Place as ShellPlace } from "@/features/shell/places";
import { t } from "@/i18n";

interface Command {
  id: string;
  group: "Issues" | "Projects" | "Pages" | "Actions" | "Answers";
  label: string;
  detail?: string;
  icon: IconName;
  run: () => void;
}

const issueKeyPattern = /^[A-Za-z][A-Za-z0-9]*-\d+$/;

export type PaletteMode = "command" | "ask";

/** What the palette points at once an answer has taken the reader somewhere. */
export interface SpotlightRequest {
  target: string;
  text: string;
}

// Ctrl+K reaches anything: a key jumps straight to the issue, words search
// summaries, and the current project's pages and a few actions are a keystroke
// away. A question gets answers from the guide; ask mode puts them first.
export function CommandPalette({
  open,
  onClose,
  projectKey,
  onNewIssue,
  onToggleSidebar,
  mode = "command",
  onSpotlight,
}: {
  open: boolean;
  onClose: () => void;
  projectKey?: string;
  onNewIssue: () => void;
  onToggleSidebar: () => void;
  mode?: PaletteMode;
  onSpotlight?: (request: SpotlightRequest) => void;
}) {
  const navigate = useNavigate();
  const { data: access } = useAccess();
  const canAdministerOrg = access?.canAdministerOrg ?? false;
  const { data: assistant } = useAssistantStatus();
  const ask = useAsk();
  const [asked, setAsked] = useState<AssistantAnswer | null>(null);
  // A change the model asked for waits here until the reader presses Confirm.
  const [proposals, setProposals] = useState<AssistantProposal[]>([]);
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const trimmed = text.trim();
  const looksLikeKey = issueKeyPattern.test(trimmed);
  const { data: currentProject } = useProject(projectKey ?? "");
  const features = currentProject?.project.features;
  const { data: projectData } = useProjectSearch(trimmed, PALETTE_RESULTS);
  const { data: issueData } = useIssues({ text: trimmed.length >= 2 && !looksLikeKey ? trimmed : undefined, limit: PALETTE_RESULTS, orderBy: "updated" });

  useEffect(() => {
    if (open) {
      setText("");
      setActive(0);
      setAsked(null);
      ask.reset();
      window.setTimeout(() => inputRef.current?.focus(), 0);
    }
    // The mutation handle is stable; listing it would rerun this on every answer.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useEffect(() => setAsked(null), [text]);

  const go = (to: string) => {
    onClose();
    navigate({ to: to as never });
  };
  const goPlace = (place: ShellPlace) => {
    onClose();
    navigate({ to: place.to, search: place.search, hash: place.hash } as never);
  };
  // The model is offered the places the guide knows, as concrete paths, so it can name one.
  const places = (): Place[] =>
    CATALOGUE.filter((entry) => entry.needs !== "admin" || canAdministerOrg).map((entry) => {
      const destination = entry.to({ projectKey, canAdministerOrg, features });
      return { id: entry.id, title: entry.title, sentence: entry.sentence, path: destination.to.replace("$projectKey", projectKey ?? "") };
    });
  const askAssistant = () => {
    ask.mutate(
      { question: trimmed, projectKey, places: places() },
      {
        onSuccess: (result) => {
          setAsked(result.answer);
          setProposals(result.proposals ?? []);
        },
      },
    );
  };
  const goThere = (found: AssistantAnswer) => {
    if (!found.to) return;
    onClose();
    navigate({ to: found.to, search: found.query ? { q: found.query } : undefined } as never);
  };
  const follow = (entry: GuideEntry) => {
    const ctx = { projectKey, canAdministerOrg, features };
    const destination = entry.needs === "project" && !projectKey ? { to: "/projects" } : entry.to(ctx);
    onClose();
    navigate({ to: destination.to, params: destination.params, search: destination.search } as never);
    if (entry.target && onSpotlight) onSpotlight({ target: entry.target, text: entry.sentence });
  };

  // Built on every render: it is cheap, and the handlers it holds are too.
  const commands = ((): Command[] => {
    const out: Command[] = [];
    const answers: Command[] =
      looksLikeQuestion(trimmed) || mode === "ask"
        ? answer(trimmed, { projectKey, canAdministerOrg, features }).map(({ entry }) => ({
            id: `answer:${entry.id}`,
            group: "Answers" as const,
            label: entry.title,
            detail: entry.needs === "project" && !projectKey ? t.shell.palette.pickProject : undefined,
            icon: "Help" as IconName,
            run: () => follow(entry),
          }))
        : [];
    if (mode === "ask") out.push(...answers);
    if (mode === "ask" && answers.length === 0 && trimmed && assistant?.configured) {
      out.push({
        id: "ask-assistant",
        group: "Answers",
        label: t.shell.palette.askAssistant,
        detail: t.shell.palette.assistantLooks,
        icon: "Help",
        run: askAssistant,
      });
    }
    if (looksLikeKey) {
      const key = trimmed.toUpperCase();
      out.push({ id: `key:${key}`, group: "Issues", label: key, detail: t.shell.palette.openIssue, icon: "Issue", run: () => go(`/issues/${key}`) });
    }
    if (trimmed.length >= 2 && !looksLikeKey) {
      for (const issue of issueData?.issues ?? []) {
        out.push({ id: `issue:${issue.key}`, group: "Issues", label: issue.summary, detail: issue.key, icon: "Issue", run: () => go(`/issues/${issue.key}`) });
      }
    }
    const lower = trimmed.toLowerCase();
    const matches = (s: string) => !lower || s.toLowerCase().includes(lower);
    // Matched again here, since the last answer stays on screen while the next is asked.
    for (const p of projectData?.projects ?? []) {
      if (matches(`${p.name} ${p.key}`))
        out.push({ id: `project:${p.key}`, group: "Projects", label: p.name, detail: p.key, icon: "Folder", run: () => go(`/projects/${p.key}`) });
    }
    if (projectKey) {
      // The setup pages are tabs of the project's settings, which every member opens; some only its administrators use.
      const listed = [
        ...pagesFor(currentProject?.project, projectWorkPages).map((page) => ({ page, address: page.path })),
        ...settingsPagesFor(currentProject?.project, canAdminister(access, projectKey)).map((page) => ({
          page,
          address: setupAddress(page.path),
        })),
      ];
      for (const { page, address } of listed) {
        if (matches(page.label))
          out.push({
            id: `page:${page.path}`,
            group: "Pages",
            label: t.shell.palette.projectPage(projectKey, page.label),
            icon: page.icon,
            run: () => go(`/projects/${projectKey}${address}`),
          });
      }
    }
    // Every page the sidebar, the avatar menu and the settings area reach, by
    // the same lists and the same rule; the empty box offers the work ones only.
    const shellPlaces: Array<{ place: ShellPlace; label: string; words: string }> = [
      ...workPlaces.map((place) => ({ place, label: place.label, words: place.label })),
      ...(lower
        ? [
            ...personalPlaces.map((place) => ({ place, label: place.label, words: `${t.shell.palette.youWords} ${place.label}` })),
            ...(canAdministerOrg
              ? [
                  {
                    place: adminEntry,
                    label: adminEntry.label,
                    words: t.shell.palette.adminWords,
                  },
                ]
              : []),
            ...adminFor(canAdministerOrg).map((place) => ({
              place,
              label: t.shell.palette.adminPage(place.label),
              words: `${t.shell.palette.adminWords} ${place.label}`,
            })),
          ]
        : []),
    ];
    for (const { place, label, words } of shellPlaces) {
      if (matches(words)) out.push({ id: `place:${placeKey(place)}`, group: "Pages", label, icon: place.icon, run: () => goPlace(place) });
    }
    const actions: Command[] = [
      {
        id: "new-issue",
        group: "Actions",
        label: t.shell.palette.newIssue,
        icon: "Plus",
        run: () => {
          onClose();
          onNewIssue();
        },
      },
      {
        id: "theme",
        group: "Actions",
        label: t.shell.palette.switchTheme,
        detail: readTheme(),
        icon: "Monitor",
        run: () => {
          const order: Theme[] = ["system", "light", "dark"];
          applyTheme(order[(order.indexOf(readTheme()) + 1) % order.length]!);
          onClose();
        },
      },
      {
        id: "sidebar",
        group: "Actions",
        label: t.shell.palette.toggleSidebar,
        icon: "Collapse",
        run: () => {
          onClose();
          onToggleSidebar();
        },
      },
      {
        id: "search",
        group: "Actions",
        label: t.shell.palette.searchQuery,
        icon: "Search",
        run: () => go(trimmed ? `/search?q=${encodeURIComponent(`text ~ "${trimmed.replace(/"/g, "")}"`)}` : "/search"),
      },
    ];
    for (const action of actions) if (matches(action.label)) out.push(action);
    if (mode !== "ask") out.push(...answers);
    return out.slice(0, PALETTE_RESULTS * 3);
  })();

  useEffect(() => setActive(0), [commands.length, trimmed]);

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive((i) => Math.min(i + 1, commands.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      commands[active]?.run();
    } else if (event.key === "Escape") {
      // Handled here, so no overlay under the palette closes with it.
      event.preventDefault();
      onClose();
    }
  }

  if (!open) return null;
  let lastGroup = "";
  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-ink/40 px-4"
      style={modalBackdropStyle}
      onPointerDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t.shell.palette.label}
        style={modalPanelStyle}
        className="flex w-full max-w-xl flex-col overflow-hidden rounded-overlay border border-border bg-surface-overlay shadow-2"
        data-palette
        data-palette-mode={mode}
      >
        <div className="flex shrink-0 items-center gap-2 border-b border-border px-3">
          <Icon.Search className="text-ink-subtle" />
          <Input
            ref={inputRef}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={onKeyDown}
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-results"
            aria-activedescendant={commands[active] ? `palette-${commands[active].id}` : undefined}
            aria-label={mode === "ask" ? t.shell.palette.askLabel : t.shell.palette.searchLabel}
            placeholder={mode === "ask" ? t.shell.palette.askPlaceholder : t.shell.palette.searchPlaceholder}
            controlSize="lg"
            className="flex-1 border-0 bg-transparent px-0!"
          />
          <kbd className="font-mono text-2xs text-ink-subtle">Esc</kbd>
        </div>
        {/* A long answer scrolls with the list rather than pushing it out of the window. */}
        <div className="max-h-[50vh] min-h-0 overflow-y-auto">
          {(ask.isPending || asked || ask.error) && <AskAnswer pending={ask.isPending} error={ask.error} answer={asked} proposals={proposals} onGo={goThere} />}
          <ul id="palette-results" role="listbox" className="p-1">
            {commands.length === 0 && (
              <li className="px-3 py-6 text-center text-sm text-ink-subtle" data-palette-empty>
                {mode === "ask" ? t.shell.palette.noAnswer : t.shell.palette.nothing}
              </li>
            )}
            {commands.map((command, index) => {
              const Glyph = Icon[command.icon];
              const heading = command.group !== lastGroup ? t.shell.palette.groups[command.group] : null;
              lastGroup = command.group;
              return (
                <li key={command.id} role="presentation">
                  {heading && <p className="px-2 pt-2 pb-1 text-2xs font-medium tracking-wide text-ink-subtle uppercase">{heading}</p>}
                  {/* Not a button: the input keeps focus and names the active option through aria-activedescendant. */}
                  <div
                    role="option"
                    id={`palette-${command.id}`}
                    aria-selected={index === active}
                    onMouseEnter={() => setActive(index)}
                    onClick={() => command.run()}
                    data-palette-option={command.id}
                    className={cx(
                      "flex w-full cursor-default items-center gap-2 rounded-control px-2 py-1.5 text-left text-sm",
                      index === active ? "bg-accent-subtle text-ink" : "text-ink hover:bg-surface-raised",
                    )}
                  >
                    <Glyph className="shrink-0 text-ink-muted" />
                    <span className="min-w-0 flex-1 truncate">{command.label}</span>
                    {command.detail && (
                      <span className={cx("shrink-0 font-mono text-2xs", index === active ? "text-ink-muted" : "text-ink-subtle")}>{command.detail}</span>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        </div>
      </div>
    </div>,
    document.body,
  );
}

// Opens the palette on Ctrl or Cmd+K anywhere in the shell, fields included:
// it is the product's one shortcut and a field has no other use for the key.
export function usePaletteShortcut(onOpen: () => void) {
  useEffect(() => {
    function onKey(event: globalThis.KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && !event.repeat && event.key.toLowerCase() === "k") {
        event.preventDefault();
        onOpen();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onOpen]);
}
