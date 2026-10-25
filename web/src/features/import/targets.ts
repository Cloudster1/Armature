import { t } from "@/i18n";

/** What each target means to a person mapping columns. */
export const TARGET_LABELS: Record<string, string> = {
  get key() {
    return t.projectSetup.import.targets.key;
  },
  get summary() {
    return t.projectSetup.import.targets.summary;
  },
  get description() {
    return t.projectSetup.import.targets.description;
  },
  get type() {
    return t.projectSetup.import.targets.type;
  },
  get status() {
    return t.projectSetup.import.targets.status;
  },
  get priority() {
    return t.projectSetup.import.targets.priority;
  },
  get assignee() {
    return t.projectSetup.import.targets.assignee;
  },
  get reporter() {
    return t.projectSetup.import.targets.reporter;
  },
  get parent() {
    return t.projectSetup.import.targets.parent;
  },
  get created() {
    return t.projectSetup.import.targets.created;
  },
  get updated() {
    return t.projectSetup.import.targets.updated;
  },
  get resolved() {
    return t.projectSetup.import.targets.resolved;
  },
  get due() {
    return t.projectSetup.import.targets.due;
  },
  get start() {
    return t.projectSetup.import.targets.start;
  },
  get estimate() {
    return t.projectSetup.import.targets.estimate;
  },
  get timeEstimate() {
    return t.projectSetup.import.targets.timeEstimate;
  },
  get timeRemaining() {
    return t.projectSetup.import.targets.timeRemaining;
  },
  get labels() {
    return t.projectSetup.import.targets.labels;
  },
};

/** Targets a file may fill from several columns; the server says the same. */
export const MANY_VALUED = new Set(["labels"]);

/** The targets whose words have to mean something here before a row is written. */
export const WORD_TARGETS = ["type", "status", "priority"];

/** What a person is asked about a name the file uses. */
export const PERSON_CHOICES = [
  {
    value: "",
    get label() {
      return t.projectSetup.import.leaveEmpty;
    },
  },
  {
    value: "create",
    get label() {
      return t.projectSetup.import.makeAccountShort;
    },
  },
];
