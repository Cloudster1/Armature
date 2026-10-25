import type { Messages } from "./en";
import { issueListDe } from "./issueList.de";
import { issuePanelsDe } from "./issuePanels.de";
import { issueViewDe } from "./issueView.de";
import { planDe } from "./plan.de";
import { settingsDe } from "./settings.de";
import { shellDe } from "./shell.de";

/** Die deutschen Texte, in der Gestalt der englischen. */
export const de: Messages = {
  common: {
    close: "Schließen",
    cancel: "Abbrechen",
    save: "Speichern",
    retry: "Erneut versuchen",
  },
  shell: shellDe,
  settings: settingsDe,
  plan: planDe,
  issueList: issueListDe,
  issueView: issueViewDe,
  issuePanels: issuePanelsDe,
};
