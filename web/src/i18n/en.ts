import { issueListEn } from "./issueList.en";
import { kitEn } from "./kit.en";
import { issuePanelsEn } from "./issuePanels.en";
import { issueViewEn } from "./issueView.en";
import { planEn } from "./plan.en";
import { settingsEn } from "./settings.en";
import { shellEn } from "./shell.en";

/** The English strings, and the shape every other language has to fill. */
export const en = {
  common: {
    close: "Close",
    cancel: "Cancel",
    save: "Save",
    retry: "Retry",
  },
  kit: kitEn,
  shell: shellEn,
  settings: settingsEn,
  plan: planEn,
  issueList: issueListEn,
  issueView: issueViewEn,
  issuePanels: issuePanelsEn,
};

export type Messages = typeof en;
