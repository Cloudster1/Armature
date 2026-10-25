import type { termsEn } from "./terms.en";

/** Wörter für Dinge, die mehrere Bereiche nennen: die Bereiche eines Tokens und wie ein Code-Host einen Pull Request nennt. */
export const termsDe: typeof termsEn = {
  scopes: {
    read: "Die Projekte und Vorgänge sehen, die Sie sehen können",
    "issue.write": "Vorgänge anlegen und bearbeiten und Seiten mit ihnen verknüpfen",
    "issue.transition": "Vorgänge durch ihren Workflow bewegen",
    "comment.write": "Vorgänge kommentieren",
    "git.write": "Code in die Branches der Vorgänge pushen, die Sie bearbeiten dürfen",
    "webhooks.partner": "Ihr die Ereignisse von Armature senden, aus jedem Projekt (nur Administratoren)",
  },
  pull: { github: "Pull Request", gitlab: "Merge Request" },
};
