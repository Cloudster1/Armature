import { LEVEL_STANDARD } from "@/api/issues";
import type { PlanItem } from "@/api/plan";
import { PLAN_ROW_HEIGHT } from "@/config";
import { flatten, type Row } from "./views";

/** Where a lifted row is held over: another issue's row, or the space above the rows. */
export type DropTarget = { kind: "row"; item: PlanItem } | { kind: "top" };

/** What a drop would do, or why it would not. */
export type Decision =
  | { kind: "into" | "beside" | "top"; parentKey: string | null }
  | { refused: string };

/** The issue row under a point, measured from the top of the first row; nothing off the rows. */
export function rowAt(rows: Row[], y: number): DropTarget | null {
  if (y < 0) return { kind: "top" };
  const row = rows[Math.floor(y / PLAN_ROW_HEIGHT)];
  if (!row || row.kind !== "issue") return null;
  return { kind: "row", item: row.item };
}

/**
 * What dropping an issue where the pointer is would mean. A row one level up
 * takes it as a child, a row on its own level lends it its parent, the space
 * above the rows makes it a root. Anything else is refused here with the
 * sentence the server would answer, so nothing is sent that would come back.
 */
export function decide(dragged: PlanItem, target: DropTarget): Decision {
  const type = dragged.issue.type;
  if (target.kind === "top") {
    if (type.level < LEVEL_STANDARD) return { refused: `A ${type.name.toLowerCase()} needs a parent issue.` };
    return { kind: "top", parentKey: null };
  }
  const over = target.item;
  if (over.issue.key === dragged.issue.key) return { refused: "An issue cannot be its own parent." };
  if (flatten(dragged.children).some((child) => child.issue.key === over.issue.key)) {
    return { refused: "That would put the issue underneath itself." };
  }
  if (over.issue.type.level === type.level + 1) return { kind: "into", parentKey: over.issue.key };
  if (over.issue.type.level === type.level) {
    if (!over.issue.parentKey && type.level < LEVEL_STANDARD) {
      return { refused: `A ${type.name.toLowerCase()} needs a parent issue.` };
    }
    return { kind: "beside", parentKey: over.issue.parentKey ?? null };
  }
  return {
    refused: `${over.issue.key} is ${article(over.issue.type.name)}, which cannot be the parent of ${article(type.name)}.`,
  };
}

function article(name: string): string {
  return (/^[aeiou]/i.test(name) ? "an " : "a ") + name;
}
