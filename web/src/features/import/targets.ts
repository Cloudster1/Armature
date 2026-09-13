/** What each target means to a person mapping columns. */
export const TARGET_LABELS: Record<string, string> = {
  key: "Key it had",
  summary: "Summary (required)",
  description: "Description",
  type: "Issue type",
  status: "Status",
  priority: "Priority",
  assignee: "Assignee",
  reporter: "Reporter",
  parent: "Parent key",
  created: "Filed on",
  updated: "Last changed on",
  resolved: "Resolved on",
  due: "Due day",
  start: "Start day",
  estimate: "Estimate",
  timeEstimate: "Time estimated",
  timeRemaining: "Time remaining",
  labels: "Labels",
};

/** Targets a file may fill from several columns; the server says the same. */
export const MANY_VALUED = new Set(["labels"]);

/** The targets whose words have to mean something here before a row is written. */
export const WORD_TARGETS = ["type", "status", "priority"];

/** What a person is asked about a name the file uses. */
export const PERSON_CHOICES = [
  { value: "", label: "Leave it empty" },
  { value: "create", label: "Make an account" },
];
