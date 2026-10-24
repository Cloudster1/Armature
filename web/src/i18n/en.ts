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
  shell: shellEn,
  settings: settingsEn,
  plan: planEn,
};

export type Messages = typeof en;
