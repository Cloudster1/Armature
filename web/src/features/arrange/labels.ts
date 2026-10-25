import type { Area, Slot } from "@/api/arrange";
import { t } from "@/i18n";

/** What each part of the page is called to somebody arranging it, in the reader's language. */
export function areaLabels(): { area: Area; title: string; note: string }[] {
  const words = t.looks.arrange.areas;
  return [
    { area: "main", title: words.main, note: words.mainNote },
    { area: "people", title: words.people, note: "" },
    { area: "planning", title: words.planning, note: "" },
    { area: "tracking", title: words.tracking, note: "" },
    { area: "more", title: words.more, note: "" },
    { area: "hidden", title: words.hidden, note: words.hiddenNote },
  ];
}

/** What each field this tracker knows is called on the page. */
export function slotLabel(slot: Slot): string {
  return t.looks.arrange.slots[slot];
}

/** Who decided the arrangement being looked at. */
export function originWords(scope: string): string {
  return t.looks.arrange.origins[scope] ?? scope;
}
