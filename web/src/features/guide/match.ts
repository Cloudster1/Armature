import { GUIDE_MAX_ANSWERS, GUIDE_MIN_SCORE, GUIDE_MIN_WORDS, GUIDE_WEIGHT_KEYWORD, GUIDE_WEIGHT_SENTENCE, GUIDE_WEIGHT_TITLE } from "@/config";
import { CATALOGUE, type GuideContext, type GuideEntry } from "./catalogue";

export interface Answer {
  entry: GuideEntry;
  score: number;
  /** How much of the title the question covered; decides between equal scores. */
  coverage: number;
}

/** Words a question is full of that say nothing about where to go. */
const STOP_WORDS = new Set([
  "a", "an", "the", "i", "do", "does", "how", "where", "what", "which", "can", "could", "is", "are", "to", "of", "in", "on", "for", "my",
  "me", "we", "you", "it", "this", "that", "there", "find", "see", "show", "get", "go", "want", "need", "please", "and", "or", "with",
  "set", "up", "into", "at", "by", "from", "all", "any", "some", "should", "would", "way", "page", "place", "thing", "something",
]);

/** Words readers use for what the product calls something else. */
const SYNONYMS: Record<string, string> = {
  tickets: "issue", ticket: "issue", bugs: "bug", task: "issue", tasks: "issue", story: "issue", stories: "issue", item: "issue", items: "issue",
  epics: "epic", swimlanes: "swimlane", kanban: "board", scrum: "board", gantt: "plan", timeline: "plan", roadmap: "plan",
  chart: "dashboard", charts: "dashboard", graph: "dashboard", graphs: "dashboard", statistics: "dashboard", stats: "dashboard", metrics: "dashboard", report: "dashboard", reports: "dashboard",
  pdf: "export", print: "export", download: "export", key: "token", keys: "token", pat: "token", apikey: "token", credentials: "token",
  password: "profile", login: "access", signin: "access", sso: "access", permissions: "role", permission: "role", rights: "role", roles: "role",
  people: "member", person: "member", user: "member", users: "member", colleague: "member", colleagues: "member", someone: "member", somebody: "member",
  status: "status", statuses: "status", state: "status", states: "status", transitions: "transition", column: "column", columns: "column",
  sprints: "sprint", iteration: "sprint", iterations: "sprint", milestones: "milestone", release: "milestone", releases: "milestone", deadline: "due",
  dark: "dark", night: "dark", colour: "theme", color: "theme", appearance: "theme", labels: "label", tags: "label", tag: "label",
  repo: "repository", repos: "repository", repositories: "repository", github: "repository", gitlab: "repository", gitea: "repository",
  assistant: "assistant", ai: "assistant", claude: "assistant", llm: "assistant", mcp: "mcp", queries: "query", nql: "query",
  dashboards: "dashboard", workflows: "workflow", projects: "project", teams: "team", fields: "field", tokens: "token", boards: "board", plans: "plan",
  notifications: "notification", alerts: "notification", alert: "notification", email: "mail", emails: "mail", mails: "mail",
  dependencies: "dependency", blockers: "blocked", blocker: "blocked", blocking: "blocked", late: "overdue", unowned: "unassigned",
};

/** A question's words, lowered, stripped of punctuation, of stop words and of a plural s. */
export function words(question: string): string[] {
  const out: string[] = [];
  for (const raw of question.toLowerCase().replace(/[^a-z0-9+\-\s]/g, " ").split(/\s+/)) {
    if (!raw || STOP_WORDS.has(raw)) continue;
    const word = SYNONYMS[raw] ?? (raw.length > 3 && raw.endsWith("s") && !raw.endsWith("ss") ? raw.slice(0, -1) : raw);
    if (!out.includes(word)) out.push(word);
  }
  return out;
}

/** Whether the typed text is a question for the guide rather than a search for an issue. */
export function looksLikeQuestion(text: string): boolean {
  const trimmed = text.trim();
  return trimmed.endsWith("?") || trimmed.split(/\s+/).filter(Boolean).length >= GUIDE_MIN_WORDS;
}

function reaches(entry: GuideEntry, ctx: GuideContext): boolean {
  if (entry.feature && ctx.features && !ctx.features.includes(entry.feature)) return false;
  if (entry.needs === "project") return true;
  if (entry.needs === "admin") return ctx.canAdministerOrg;
  return true;
}

/**
 * The answers to a question, best first, or none: a word that hits a title
 * outweighs one that hits a keyword, which outweighs one found in the sentence.
 */
export function answer(question: string, ctx: GuideContext, entries: GuideEntry[] = CATALOGUE): Answer[] {
  const asked = words(question);
  if (asked.length === 0) return [];
  const out: Answer[] = [];
  for (const entry of entries) {
    if (!reaches(entry, ctx)) continue;
    const title = words(entry.title);
    const keywords = new Set([...entry.keywords.map((k) => SYNONYMS[k] ?? k), ...entry.keywords]);
    const sentence = new Set(words(entry.sentence));
    let score = 0;
    let titleHits = 0;
    for (const word of asked) {
      if (title.includes(word)) {
        score += GUIDE_WEIGHT_TITLE;
        titleHits += 1;
      } else if (keywords.has(word)) score += GUIDE_WEIGHT_KEYWORD;
      else if (sentence.has(word)) score += GUIDE_WEIGHT_SENTENCE;
    }
    const normalised = score / asked.length;
    if (normalised >= GUIDE_MIN_SCORE) out.push({ entry, score: normalised, coverage: title.length ? titleHits / title.length : 0 });
  }
  out.sort((a, b) => b.score - a.score || b.coverage - a.coverage || a.entry.title.localeCompare(b.entry.title));
  return out.slice(0, GUIDE_MAX_ANSWERS);
}
