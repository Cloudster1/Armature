import type { RequestType } from "@/api/desk";

/** The heading for request types that were given no category. */
export const GENERAL_CATEGORY = "General";

export interface CategoryGroup {
  name: string;
  types: RequestType[];
}

/**
 * Groups request types under their categories, case-insensitively, keeping
 * the order the desk offers them in: the general list first, then each
 * category as it is first met. The spelling shown is the first one seen.
 */
export function groupByCategory(types: RequestType[]): CategoryGroup[] {
  const groups = new Map<string, CategoryGroup>();
  for (const type of types) {
    const name = type.category.trim() || GENERAL_CATEGORY;
    const id = name.toLowerCase();
    const group = groups.get(id) ?? { name, types: [] };
    group.types.push(type);
    groups.set(id, group);
  }
  const general = groups.get(GENERAL_CATEGORY.toLowerCase());
  const rest = [...groups.values()].filter((group) => group !== general);
  return general ? [general, ...rest] : rest;
}

/**
 * What the details box should hold after a template was chosen: the new
 * template when the box is empty or still holds the previous one, otherwise
 * what the requester has typed, which a template must never overwrite.
 */
export function prefillDetails(current: string, previousTemplate: string, nextTemplate: string): string {
  if (current.trim() === "" || current === previousTemplate) return nextTemplate;
  return current;
}

/** The categories in use, for suggesting to an administrator. */
export function categoriesOf(types: RequestType[]): string[] {
  return groupByCategory(types)
    .map((group) => group.name)
    .filter((name) => name !== GENERAL_CATEGORY);
}
