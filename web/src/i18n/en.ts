import { termsEn } from "./terms.en";
import { deskEn } from "./desk.en";
import { guideEn } from "./guide.en";
import { workflowsEn } from "./workflows.en";
import { accessEn } from "./access.en";
import { looksEn } from "./looks.en";
import { projectSetupEn } from "./projectSetup.en";
import { doorsEn } from "./doors.en";
import { boardEn } from "./board.en";
import { dashboardEn } from "./dashboard.en";
import { scheduleEn } from "./schedule.en";
import { searchEn } from "./search.en";
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
  terms: termsEn,
  shell: shellEn,
  settings: settingsEn,
  plan: planEn,
  issueList: issueListEn,
  issueView: issueViewEn,
  issuePanels: issuePanelsEn,
  board: boardEn,
  dashboard: dashboardEn,
  schedule: scheduleEn,
  search: searchEn,
  workflows: workflowsEn,
  access: accessEn,
  looks: looksEn,
  projectSetup: projectSetupEn,
  doors: doorsEn,
  desk: deskEn,
  guide: guideEn,
};

export type Messages = typeof en;
