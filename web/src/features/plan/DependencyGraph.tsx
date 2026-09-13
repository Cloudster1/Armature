import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useAddLink, usePlan, useRemoveLink, type Dependency, type PlanItem } from "@/api/plan";
import { Card, ErrorBanner, IconButton, cx } from "@/components/ui";
import { Icon } from "@/components/icons";
import { TypeBadge } from "@/features/issues/badges";
import { EDGE_LINGER_MS, GRAPH_MIN_GRID_COLUMNS, GRAPH_NODE_WIDTH, GRAPH_ORIGIN, GRAPH_ROW_GAP } from "@/config";
import { layoutGraph, type GraphEdge, type GraphNode } from "./graph";
import { NO_CUT, NO_FILTERS, cutItems, filterItems, flatten, type Cut, type Filters } from "./views";

/** What the pane is assumed to measure before it has been measured. */
const FALLBACK_PANE_WIDTH = 900;

/** A dependency being drawn from one node towards the pointer, in canvas pixels. */
interface Draft {
  from: string;
  x0: number;
  y0: number;
  x: number;
  y: number;
  to: string | null;
}

/**
 * The project's tickets as a graph of what waits on what: linked tickets in
 * columns by longest path, the rest in a grid underneath. The same filters
 * and cut as the timeline apply, so it is the same plan read a third way.
 */
export function DependencyGraph({
  projectKey,
  query = "",
  cut: scope = NO_CUT,
  filters = NO_FILTERS,
}: {
  projectKey: string;
  query?: string;
  cut?: Cut;
  /** What the toolbar narrowed the nodes to; the toolbar owns the state. */
  filters?: Filters;
}) {
  const { data, isLoading, error } = usePlan(projectKey, query);
  const addLink = useAddLink();
  const removeLink = useRemoveLink();
  const [selected, setSelected] = useState<Dependency | null>(null);
  // An edge stays hot a moment after the pointer leaves it, so its remove
  // control, which sits on the edge, can be reached.
  const [hovered, setHovered] = useState<string | null>(null);
  const linger = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hot = (edgeId: string) => {
    if (linger.current) clearTimeout(linger.current);
    setHovered(edgeId);
  };
  const cool = () => {
    if (linger.current) clearTimeout(linger.current);
    linger.current = setTimeout(() => setHovered(null), EDGE_LINGER_MS);
  };
  const [draft, setDraft] = useState<Draft | null>(null);
  const draftRef = useRef<{ from: string; x0: number; y0: number } | null>(null);
  const canvasRef = useRef<HTMLDivElement>(null);

  const [pane, setPane] = useState<HTMLDivElement | null>(null);
  const [available, setAvailable] = useState(0);
  useEffect(() => {
    if (!pane) return;
    const measure = () => setAvailable(pane.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(pane);
    return () => observer.disconnect();
  }, [pane]);

  const cut = useMemo<Cut>(
    () => ({ ...scope, matched: query && data ? new Set(data.matched) : null }),
    [scope, query, data],
  );
  const shown = useMemo(() => filterItems(cutItems(data?.items ?? [], cut, new Date()), filters), [data?.items, cut, filters]);
  const visible = useMemo(() => new Map(flatten(shown).map((item) => [item.issue.key, item] as const)), [shown]);
  const inProject = useMemo(() => new Set(flatten(data?.items ?? []).map((item) => item.issue.key)), [data?.items]);

  // A blocker in another project has no row of its own; it is drawn as a
  // dashed node when what it blocks is on the page.
  const { nodes, edges, externals } = useMemo(() => {
    const externals = new Set<string>();
    const edges: GraphEdge[] = [];
    for (const dep of data?.dependencies ?? []) {
      const ends = [dep.blockerKey, dep.blockedKey];
      const drawn = ends.map((key) => visible.has(key) || !inProject.has(key));
      if (!drawn[0] || !drawn[1] || (!visible.has(dep.blockerKey) && !visible.has(dep.blockedKey))) continue;
      for (const key of ends) if (!inProject.has(key)) externals.add(key);
      edges.push({ id: dep.linkId, from: dep.blockerKey, to: dep.blockedKey });
    }
    const nodes: GraphNode[] = [...visible.keys(), ...externals].map((key) => ({ key }));
    return { nodes, edges, externals };
  }, [data?.dependencies, visible, inProject]);

  const paneWidth = available || FALLBACK_PANE_WIDTH;
  const columns = Math.max(GRAPH_MIN_GRID_COLUMNS, Math.floor((paneWidth - GRAPH_ORIGIN) / (GRAPH_NODE_WIDTH + GRAPH_ROW_GAP)));
  const layout = useMemo(() => layoutGraph(nodes, edges, { columns }), [nodes, edges, columns]);
  const dependencyById = useMemo(() => new Map((data?.dependencies ?? []).map((d) => [d.linkId, d])), [data?.dependencies]);

  if (isLoading) return <p className="text-sm text-ink-muted">Loading the plan...</p>;
  if (error && !data) return <ErrorBanner>{(error as Error).message}</ErrorBanner>;
  if (!data) return null;

  function at(event: ReactPointerEvent<HTMLElement>): { x: number; y: number } {
    const box = canvasRef.current?.getBoundingClientRect();
    return { x: event.clientX - (box?.left ?? 0), y: event.clientY - (box?.top ?? 0) };
  }

  /** The node under the pointer that a dependency could end on: in this project, and not where it began. */
  function nodeUnder(event: ReactPointerEvent<HTMLElement>, from: string): string | null {
    const el = document.elementFromPoint(event.clientX, event.clientY)?.closest<HTMLElement>("[data-graph-node]");
    const key = el?.dataset.graphNode;
    return key && key !== from && inProject.has(key) ? key : null;
  }

  function startLink(event: ReactPointerEvent<HTMLElement>, from: string) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const { x, y } = at(event);
    draftRef.current = { from, x0: x, y0: y };
    setDraft({ from, x0: x, y0: y, x, y, to: null });
    try {
      event.currentTarget.setPointerCapture(event.pointerId);
    } catch {
      // No capture; the canvas's own handlers carry the drag.
    }
  }

  function onPointerMove(event: ReactPointerEvent<HTMLElement>) {
    const drawing = draftRef.current;
    if (!drawing) return;
    const { x, y } = at(event);
    setDraft({ ...drawing, x, y, to: nodeUnder(event, drawing.from) });
  }

  function onPointerUp(event: ReactPointerEvent<HTMLElement>) {
    const drawing = draftRef.current;
    draftRef.current = null;
    setDraft(null);
    if (!drawing) return;
    const to = nodeUnder(event, drawing.from);
    if (to) addLink.mutate({ key: drawing.from, type: "Blocks", targetKey: to });
  }

  const selectedStill = selected && dependencyById.get(selected.linkId);
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <p className="text-sm text-ink-muted">
          {edges.length === 0
            ? "Nothing waits on anything yet. Drag the dot at a ticket's edge onto another to say it must finish first."
            : `${edges.length} ${edges.length === 1 ? "dependency" : "dependencies"} among ${nodes.length} tickets`}
        </p>
        {addLink.error && <ErrorBanner>{(addLink.error as Error).message}</ErrorBanner>}
        {removeLink.error && <ErrorBanner>{(removeLink.error as Error).message}</ErrorBanner>}
      </div>

      <Card className="overflow-hidden">
        <div ref={setPane} className="overflow-auto" data-testid="graph-pane">
          <div
            ref={canvasRef}
            className={cx("relative", draft && "select-none")}
            style={{ width: Math.max(layout.width, paneWidth), height: layout.height }}
            data-testid="graph-canvas"
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={onPointerUp}
          >
            <svg
              className="pointer-events-none absolute top-0 left-0 overflow-visible"
              width={Math.max(layout.width, paneWidth)}
              height={layout.height}
              aria-hidden="true"
              data-testid="graph-edges"
            >
              <defs>
                <marker id="graph-arrow" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto">
                  <path d="M0,0 L6,3 L0,6 z" className="fill-ink-subtle" />
                </marker>
                <marker id="graph-arrow-selected" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto">
                  <path d="M0,0 L6,3 L0,6 z" className="fill-accent" />
                </marker>
                <marker id="graph-arrow-cycle" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto">
                  <path d="M0,0 L6,3 L0,6 z" className="fill-danger" />
                </marker>
              </defs>
              {layout.edges.map((edge) => {
                const isSelected = selected?.linkId === edge.id;
                const isHot = isSelected || hovered === edge.id;
                const marker = isHot ? "graph-arrow-selected" : edge.cycle ? "graph-arrow-cycle" : "graph-arrow";
                return (
                  <g key={edge.id}>
                    <path
                      d={edge.path}
                      fill="none"
                      strokeWidth={isHot ? 2.5 : 1.5}
                      strokeDasharray={edge.cycle ? "5 4" : undefined}
                      markerEnd={`url(#${marker})`}
                      className={isHot ? "stroke-accent" : edge.cycle ? "stroke-danger" : "stroke-ink-subtle"}
                      data-graph-edge={`${edge.from}->${edge.to}`}
                      data-graph-cycle={edge.cycle || undefined}
                      data-selected={isSelected || undefined}
                    />
                    <path
                      d={edge.path}
                      fill="none"
                      stroke="transparent"
                      strokeWidth="14"
                      className="pointer-events-auto cursor-pointer"
                      data-graph-edge-hit={`${edge.from}->${edge.to}`}
                      onClick={() => setSelected(dependencyById.get(edge.id) ?? null)}
                      onPointerEnter={() => hot(edge.id)}
                      onPointerLeave={cool}
                    >
                      <title>
                        {edge.from} blocks {edge.to}
                        {edge.cycle ? ", which comes back around" : ""}
                      </title>
                    </path>
                  </g>
                );
              })}
              {draft && (
                <line
                  x1={draft.x0}
                  y1={draft.y0}
                  x2={draft.x}
                  y2={draft.y}
                  strokeWidth="1.5"
                  strokeDasharray="4 3"
                  className={draft.to ? "stroke-accent" : "stroke-ink-subtle"}
                  data-graph-draft={draft.from}
                />
              )}
            </svg>

            {(() => {
              const shownEdge = layout.edges.find((e) => e.id === (hovered ?? selectedStill?.linkId));
              const shown = shownEdge && dependencyById.get(shownEdge.id);
              if (!shownEdge || !shown) return null;
              return (
                <IconButton
                  size="xs"
                  icon={<Icon.X />}
                  label={`Remove dependency ${shown.blockerKey} blocks ${shown.blockedKey}`}
                  className="absolute z-10 -translate-x-1/2 -translate-y-1/2 border border-accent bg-surface text-accent shadow-sm hover:bg-accent-subtle"
                  style={{ left: shownEdge.handle.x, top: shownEdge.handle.y }}
                  data-graph-remove
                  data-graph-unlink={shown.linkId}
                  disabled={removeLink.isPending}
                  onPointerEnter={() => hot(shownEdge.id)}
                  onPointerLeave={cool}
                  onClick={() => removeLink.mutate({ key: shown.blockerKey, linkId: shown.linkId }, { onSuccess: () => setSelected(null) })}
                />
              );
            })()}

            {layout.unlinkedTop !== null && (
              <span
                className="absolute text-2xs font-semibold tracking-wide text-ink-subtle uppercase"
                style={{ left: GRAPH_ORIGIN, top: layout.unlinkedTop }}
                data-graph-unlinked
              >
                Not linked to anything
              </span>
            )}

            {[...layout.nodes.entries()].map(([key, rect]) => {
              const item = visible.get(key);
              return (
                <GraphNode
                  key={key}
                  issueKey={key}
                  item={item}
                  external={externals.has(key)}
                  rect={rect}
                  target={draft?.to === key}
                  onStartLink={(event) => startLink(event, key)}
                />
              );
            })}
          </div>
        </div>
      </Card>
    </div>
  );
}

const tone: Record<string, string> = {
  todo: "border-l-border-strong",
  in_progress: "border-l-accent",
  done: "border-l-success",
};

function GraphNode({
  issueKey,
  item,
  external,
  rect,
  target,
  onStartLink,
}: {
  issueKey: string;
  item: PlanItem | undefined;
  external: boolean;
  rect: { x: number; y: number; width: number; height: number };
  target: boolean;
  onStartLink: (event: ReactPointerEvent<HTMLElement>) => void;
}) {
  return (
    <div
      className={cx(
        "absolute flex items-center gap-1.5 rounded border bg-surface px-2 text-sm shadow-sm",
        external ? "border-dashed border-border-strong text-ink-muted" : cx("border-border border-l-4", tone[item?.issue.status.category ?? "todo"]),
        target && "ring-2 ring-accent",
      )}
      style={{ left: rect.x, top: rect.y, width: rect.width, height: rect.height }}
      data-graph-node={issueKey}
      data-graph-external={external || undefined}
    >
      {item && <TypeBadge icon={item.issue.type.icon} name={item.issue.type.name} />}
      <Link to="/issues/$issueKey" params={{ issueKey }} className="shrink-0 font-mono text-2xs text-ink-muted hover:text-accent">
        {issueKey}
      </Link>
      <span className="min-w-0 flex-1 truncate text-ink" title={item?.issue.summary}>
        {external ? "in another project" : item?.issue.summary}
      </span>
      {!external && (
        <span
          role="button"
          aria-label={`Drag to make ${issueKey} block another issue`}
          title="Drag onto another ticket: this must finish before that starts"
          data-graph-handle={issueKey}
          onPointerDown={onStartLink}
          className="absolute top-1/2 -right-1.5 size-3 -translate-y-1/2 cursor-crosshair rounded-full border-2 border-accent bg-surface hover:bg-accent-subtle"
        />
      )}
    </div>
  );
}
