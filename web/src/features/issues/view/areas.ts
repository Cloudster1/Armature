import type { Area, Placement } from "@/api/arrange";

/** The groups the facts fold into, and what each is called on the page. */
export const ASIDE_AREAS: { area: Area; title: string }[] = [
  { area: "people", title: "People" },
  { area: "planning", title: "Planning" },
  { area: "tracking", title: "Tracking" },
  { area: "more", title: "Fields" },
];

/** What a place is called: a slot this tracker knows, or one of the project's own fields. */
export function keyOf(place: Placement): string {
  return place.fieldId ? `custom:${place.fieldId}` : (place.slot ?? "");
}

export function placesIn(places: Placement[], area: Area): Placement[] {
  return places.filter((place) => place.area === area);
}

// Every field the arrangement names, wherever it put it, including what it
// hid: those are the ones the catch-all slot leaves out.
export function namedFields(places: Placement[]): string[] {
  return places.flatMap((place) => (place.fieldId ? [place.fieldId] : []));
}
