import type { Feature } from "@/api/projects";
import { projectSetupPages, projectWorkPages } from "@/features/shell/Sidebar";
import { setupAddress } from "@/features/projects/settingsTabs";
import { t } from "@/i18n";

/** What the guide knows about the reader when it answers. */
export interface GuideContext {
  projectKey?: string;
  canAdministerOrg: boolean;
  /** The current project's features; absent when no project is known, which hides nothing. */
  features?: Feature[];
}

/** Where an answer takes the reader; params and search as the router takes them. */
export interface Destination {
  to: string;
  params?: Record<string, string>;
  search?: Record<string, unknown>;
}

/**
 * One thing the guide can answer: where it is, what it is for, the words a
 * question might use, and which element on the page to point at once there.
 */
export interface GuideEntry {
  id: string;
  title: string;
  sentence: string;
  keywords: string[];
  to: (ctx: GuideContext) => Destination;
  needs?: "project" | "admin";
  /** The feature the entry belongs to; a project without it is not told about it. */
  feature?: Feature;
  /** The data-guide value of the element the callout points at, when the page has one. */
  target?: string;
}

const PROJECT_PAGE_WORDS: Record<string, { keywords: string[]; target?: string }> = {
  "": { keywords: ["issue", "ticket", "list", "table", "filter", "open"] },
  "/board": {
    keywords: ["board", "kanban", "scrum", "column", "card", "swimlane", "drag"],
  },
  "/sprints": { keywords: ["sprint", "iteration", "velocity", "commit", "backlog"] },
  "/plan": {
    keywords: ["plan", "gantt", "timeline", "schedule", "dependency", "late", "roadmap"],
  },
  "/calendar": {
    keywords: ["calendar", "month", "week", "day", "date", "due", "agenda", "when"],
  },
  "/resources": {
    keywords: ["resource", "capacity", "allocation", "workload", "utilisation", "overloaded", "hours", "people", "availability"],
  },
  "/milestones": {
    keywords: ["milestone", "release", "due", "overdue", "progress", "deadline"],
  },
  "/dashboard": {
    keywords: ["dashboard", "chart", "widget", "report", "burndown", "throughput", "graph", "statistic", "metric"],
  },
  "/hierarchy": {
    keywords: ["hierarchy", "epic", "tree", "parent", "child", "breakdown"],
  },
  "/queues": { keywords: ["queue", "request", "desk", "waiting", "sla"] },
  "/service-desk": {
    keywords: ["desk", "portal", "request", "mail", "customer", "support"],
  },
  "/teams": { keywords: ["team", "member", "capacity", "people"] },
  "/workflows": {
    keywords: ["workflow", "status", "transition", "scheme", "type", "map", "graph", "flow"],
    target: "workflow-decide",
  },
  "/fields": {
    keywords: ["field", "custom", "column", "attribute", "property"],
  },
  "/issue-types": {
    keywords: ["issue type", "type", "allow", "bug", "epic", "task", "story", "order"],
  },
  "/repositories": {
    keywords: ["repository", "git", "branch", "commit", "webhook", "gitea", "github", "gitlab", "pull"],
  },
  "/releases": {
    keywords: ["release", "version", "ship", "fix version", "changelog", "notes", "release notes", "deploy"],
    target: "new-version",
  },
  "/components": {
    keywords: ["component", "part", "module", "area", "owner", "lead", "default assignee"],
    target: "new-component",
  },
  "/import": {
    keywords: ["import", "csv", "file", "upload", "spreadsheet", "excel", "migrate", "bulk create"],
    target: "import",
  },
  "/automation": {
    keywords: ["automation", "rule", "automate", "trigger", "action", "schedule", "run", "log"],
    target: "new-rule",
  },
  "/settings": { keywords: ["project", "setting", "rename", "lead", "archive", "delete"] },
};

type Said = { title: string; sentence: string };

/** An answer's words in the reader's language, read when they are shown. */
function said(id: string): Said {
  return (t.guide.entries as Record<string, Said>)[id] ?? { title: id, sentence: "" };
}

/** An answer whose title and sentence follow the language, as getters over the catalogue. */
function entry(answer: Omit<GuideEntry, "title" | "sentence">): GuideEntry {
  return Object.defineProperties(answer, {
    title: { get: () => said(answer.id).title, enumerable: true },
    sentence: { get: () => said(answer.id).sentence, enumerable: true },
  }) as GuideEntry;
}

/** One of a project's pages, named as the sidebar names it. */
function projectPage(page: { path: string; label: string; feature?: Feature }, address = page.path): GuideEntry {
  const words = PROJECT_PAGE_WORDS[page.path] ?? { keywords: [] };
  return {
    id: `page:${page.path || "/issues"}`,
    get title() {
      return page.label;
    },
    get sentence() {
      return (t.guide.projectPages as Record<string, string>)[page.path] ?? t.guide.projectPageFallback(page.label);
    },
    get keywords() {
      return [page.label.toLowerCase(), ...words.keywords];
    },
    needs: "project",
    feature: page.feature,
    target: words.target,
    to: (ctx) => ({ to: `/projects/$projectKey${address}`, params: { projectKey: ctx.projectKey ?? "" } }),
  };
}

const pages: GuideEntry[] = [
  entry({
    id: "page:home",
    keywords: ["home", "start", "overview", "mine", "recent"],
    to: () => ({ to: "/" }),
  }),
  entry({
    id: "page:projects",
    keywords: ["project", "new", "create", "template", "list"],
    to: () => ({ to: "/projects" }),
  }),
  entry({
    id: "page:search",
    keywords: ["search", "query", "nql", "find", "filter", "syntax", "help", "language"],
    target: "query",
    to: () => ({ to: "/search", search: { q: "" } }),
  }),
  ...projectWorkPages.map((page) => projectPage(page)),
  ...projectSetupPages.map((page) => projectPage(page, setupAddress(page.path))),
  entry({
    id: "page:filters",
    keywords: ["saved", "search", "filter", "query", "subscription", "subscribe", "star", "starred", "mail"],
    to: () => ({ to: "/filters" }),
  }),
  entry({
    id: "page:inbox",
    keywords: ["inbox", "notification", "bell", "unread", "told", "mention", "mentioned"],
    target: "inbox",
    to: () => ({ to: "/inbox" }),
  }),
  entry({
    id: "page:settings",
    keywords: ["setting", "preference", "configure"],
    to: () => ({ to: "/settings" }),
  }),
  entry({
    id: "page:admin",
    keywords: ["admin", "administration", "administrator", "installation", "everyone", "configure"],
    needs: "admin",
    to: () => ({ to: "/admin" }),
  }),
  entry({
    id: "page:profile",
    keywords: ["profile", "name", "picture", "avatar", "photo", "timezone", "zone", "language", "locale", "date", "account", "me"],
    to: () => ({ to: "/settings/profile" }),
  }),
  entry({
    id: "page:tokens",
    keywords: ["token", "api", "key", "pat", "script", "ci", "mcp", "assistant", "bearer", "read-only", "readonly", "secret"],
    target: "token-form",
    to: () => ({ to: "/settings/tokens" }),
  }),
  entry({
    id: "page:installation",
    keywords: ["installation", "name", "rename", "title", "instance", "site"],
    to: () => ({ to: "/admin/installation" }),
  }),
  entry({
    id: "page:access",
    keywords: ["access", "role", "permission", "group", "invite", "invitation", "member", "user", "sso", "oidc", "keycloak", "login", "right", "administrator"],
    needs: "admin",
    to: () => ({ to: "/admin/access" }),
  }),
  entry({
    id: "page:org-workflows",
    keywords: ["workflow", "scheme", "status", "transition", "design", "designer", "canvas", "all projects"],
    needs: "admin",
    to: () => ({ to: "/admin/workflows" }),
  }),
  entry({
    id: "page:org-automation",
    keywords: ["automation", "rule", "automate", "trigger", "when", "then", "all projects"],
    needs: "admin",
    to: () => ({ to: "/admin/automation" }),
  }),
  entry({
    id: "page:webhooks",
    keywords: ["webhook", "hook", "post", "endpoint", "slack", "teams", "chat", "integration", "event", "signature", "secret", "delivery"],
    needs: "admin",
    target: "new-webhook",
    to: () => ({ to: "/admin/webhooks" }),
  }),
  entry({
    id: "page:holidays",
    keywords: ["holiday", "holidays", "calendar", "day off", "vacation", "public", "bank", "ics", "working week", "hours", "part-time"],
    to: () => ({ to: "/admin/holidays" }),
  }),
  entry({
    id: "page:labels",
    keywords: ["label", "tag", "word"],
    to: () => ({ to: "/admin/labels" }),
  }),
  entry({
    id: "page:project-templates",
    keywords: ["template", "project template", "scrum", "kanban", "duplicate", "scheme", "issue types", "new project"],
    needs: "admin",
    to: () => ({ to: "/admin/project-templates" }),
  }),
];

const actions: GuideEntry[] = [
  entry({
    id: "action:new-issue",
    keywords: ["create", "new", "file", "add", "issue", "ticket", "bug", "task", "story", "report"],
    target: "new-issue",
    to: (ctx) => (ctx.projectKey ? { to: "/projects/$projectKey", params: { projectKey: ctx.projectKey } } : { to: "/projects" }),
  }),
  entry({
    id: "action:share-dashboard",
    keywords: ["share", "link", "public", "external", "monitor", "screen", "wall", "revoke", "dashboard"],
    needs: "project",
    target: "share",
    to: (ctx) => ({ to: "/projects/$projectKey/dashboard", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:export-pdf",
    keywords: ["export", "pdf", "print", "download", "file", "dashboard"],
    needs: "project",
    target: "export-pdf",
    to: (ctx) => ({ to: "/projects/$projectKey/dashboard", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:dashboard-template",
    keywords: ["template", "dashboard", "save", "reuse", "copy", "new"],
    needs: "project",
    target: "add-dashboard",
    to: (ctx) => ({ to: "/projects/$projectKey/dashboard", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:milestone-dashboard",
    keywords: ["milestone", "dashboard", "progress", "release", "track"],
    needs: "project",
    target: "milestone-dashboard",
    to: (ctx) => ({ to: "/projects/$projectKey/milestones", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:filter-dashboard",
    keywords: ["filter", "narrow", "team", "type", "window", "days", "dashboard", "tile"],
    needs: "project",
    target: "arrange",
    to: (ctx) => ({ to: "/projects/$projectKey/dashboard", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:map-workflow",
    keywords: ["workflow", "type", "map", "assign", "decide", "change", "different", "own", "bug", "status"],
    needs: "project",
    target: "workflow-decide",
    to: (ctx) => ({ to: "/projects/$projectKey/settings/workflows", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:design-workflow",
    keywords: ["design", "draw", "workflow", "status", "transition", "canvas", "new", "create", "edit"],
    needs: "admin",
    to: () => ({ to: "/admin/workflows/new" }),
  }),
  entry({
    id: "action:create-token",
    keywords: ["token", "api", "key", "create", "script", "ci", "mcp", "assistant", "read-only", "readonly", "claude"],
    target: "token-form",
    to: () => ({ to: "/settings/tokens" }),
  }),
  entry({
    id: "action:notification-settings",
    keywords: ["mail", "digest", "bundle", "quiet", "mute", "off", "preference", "notification", "settings"],
    target: "notification-settings",
    to: () => ({ to: "/settings/profile" }),
  }),
  entry({
    id: "action:mention",
    keywords: ["mention", "@", "name", "ping", "tag", "notify", "comment", "watcher", "tell"],
    needs: "project",
    to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:automation-rule",
    keywords: [
      "automation",
      "rule",
      "automate",
      "trigger",
      "when",
      "then",
      "label automatically",
      "assign automatically",
      "schedule",
      "cron",
      "workflow automation",
      "run log",
    ],
    needs: "project",
    target: "new-rule",
    to: (ctx) => ({ to: "/projects/$projectKey/settings/automation", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:save-search",
    keywords: ["save", "search", "filter", "query", "named", "share", "remember"],
    target: "save-search",
    to: () => ({ to: "/search", search: { q: "statusCategory != done" } }),
  }),
  entry({
    id: "action:export-csv",
    keywords: ["export", "csv", "file", "download", "spreadsheet", "excel", "issues", "one issue", "this issue", "selected", "ticked", "list"],
    target: "export-csv",
    to: () => ({ to: "/search", search: { q: "" } }),
  }),
  entry({
    id: "action:bulk",
    keywords: ["bulk", "many", "several", "multiple", "batch", "select", "at once", "mass"],
    to: () => ({ to: "/search", search: { q: "" } }),
  }),
  entry({
    id: "action:clone",
    keywords: ["clone", "copy", "duplicate"],
    needs: "project",
    to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:move",
    keywords: ["move", "transfer", "relocate", "elsewhere"],
    needs: "project",
    to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:knowledge-base",
    keywords: ["article", "knowledge", "knowledge base", "kb", "faq", "self service", "help article", "publish"],
    needs: "project",
    target: "knowledge-base",
    to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:canned",
    keywords: ["canned", "canned response", "saved reply", "template reply", "macro", "snippet", "boilerplate"],
    needs: "project",
    target: "canned-responses",
    to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:business-hours",
    keywords: [
      "business hours",
      "calendar",
      "working hours",
      "office hours",
      "weekend",
      "holiday",
      "holiday calendar",
      "time zone",
      "timezone",
      "sla calendar",
    ],
    needs: "project",
    target: "business-hours",
    to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:desk-companies",
    keywords: ["company", "companies", "colleague", "colleagues", "share", "shared", "domain", "group customers"],
    needs: "project",
    target: "desk-companies",
    to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:desk-door",
    keywords: [
      "door",
      "code",
      "mail verification",
      "verify",
      "verification",
      "without a code",
      "no code",
      "portal address",
      "desk address",
      "walk in",
      "trust",
    ],
    needs: "project",
    target: "desk-door",
    to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:csat",
    keywords: ["csat", "satisfaction", "rating", "rated", "score", "survey", "feedback", "happy", "nps"],
    needs: "project",
    to: (ctx) => ({ to: "/projects/$projectKey/queues", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:audit",
    keywords: ["audit", "log", "history", "who", "granted", "changed", "security", "compliance", "trail", "record"],
    needs: "admin",
    to: () => ({ to: "/admin/audit" }),
  }),
  entry({
    id: "action:org-field",
    keywords: ["org field", "global field", "shared field", "every project", "promote", "all projects"],
    needs: "admin",
    to: () => ({ to: "/admin/fields" }),
  }),
  entry({
    id: "action:status-update",
    keywords: ["status update", "health", "on track", "at risk", "off track", "how is the project", "progress report", "update"],
    needs: "project",
    target: "post-status",
    to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:theme",
    keywords: ["theme", "dark", "light", "mode", "colour", "color", "appearance", "night"],
    target: "theme",
    to: () => ({ to: "/settings/profile" }),
  }),
  entry({
    id: "action:demo-project",
    keywords: ["demo", "example", "sample", "try", "show", "showcase", "data", "factory", "seed"],
    needs: "admin",
    target: "add-demo-project",
    to: () => ({ to: "/admin/installation" }),
  }),
  entry({
    id: "action:invite",
    keywords: ["invite", "invitation", "add", "people", "user", "member", "colleague", "join"],
    needs: "admin",
    to: () => ({ to: "/admin/access" }),
  }),
  entry({
    id: "action:query-help",
    keywords: ["query", "nql", "syntax", "help", "field", "operator", "function", "currentuser", "search", "language", "write"],
    target: "query",
    to: () => ({ to: "/search", search: { q: "" } }),
  }),
  entry({
    id: "action:issue-beside",
    keywords: ["panel", "drawer", "beside", "side", "preview", "quick", "arrow", "keyboard", "open", "issue"],
    needs: "project",
    to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }),
  }),
  entry({
    id: "action:palette",
    keywords: ["palette", "shortcut", "ctrl", "cmd", "keyboard", "command", "jump", "quick", "fast"],
    to: () => ({ to: "/" }),
  }),
];

/** A question that is really a query: the words compile to a search. */
function intent(id: string, keywords: string[], q: string): GuideEntry {
  return entry({ id: `intent:${id}`, keywords, to: () => ({ to: "/search", search: { q } }) });
}

const intents: GuideEntry[] = [
  intent(
    "mine",
    ["my", "mine", "me", "assigned", "open", "issue", "work", "todo"],
    "assignee = currentUser() AND statusCategory != done ORDER BY priority DESC",
  ),
  intent("reported", ["reported", "filed", "created", "my", "me", "raised"], "reporter = currentUser() ORDER BY created DESC"),
  intent("due-week", ["due", "week", "deadline", "soon", "upcoming"], "due <= endOfWeek() AND statusCategory != done ORDER BY due ASC"),
  intent("overdue", ["overdue", "late", "past", "due", "missed", "behind"], "due < startOfDay() AND statusCategory != done ORDER BY due ASC"),
  intent("today", ["today", "updated", "changed", "recent", "activity", "happened"], "updated >= startOfDay() ORDER BY updated DESC"),
  intent("unassigned", ["unassigned", "nobody", "free", "unowned", "assignee", "empty"], "assignee IS EMPTY AND statusCategory != done"),
  intent(
    "urgent",
    ["urgent", "priority", "highest", "high", "critical", "important"],
    "priority IN (high, highest) AND statusCategory != done ORDER BY priority DESC",
  ),
  intent("bugs", ["bug", "defect", "broken", "open"], "type = Bug AND statusCategory != done ORDER BY priority DESC"),
  intent("in-progress", ["progress", "doing", "working", "active", "started", "now"], "statusCategory = in_progress ORDER BY updated DESC"),
  intent("done-week", ["done", "finished", "resolved", "closed", "completed", "week", "shipped"], "resolved >= startOfWeek() ORDER BY resolved DESC"),
  intent("no-estimate", ["estimate", "unestimated", "sized", "points", "missing"], "estimate IS EMPTY AND statusCategory != done"),
  entry({
    id: "intent:blocked",
    keywords: ["blocked", "blocker", "blocking", "depends", "dependency", "waiting", "stuck"],
    needs: "project",
    to: (ctx) => ({ to: "/projects/$projectKey/plan", params: { projectKey: ctx.projectKey ?? "" }, search: { view: "dependencies" } }),
  }),
];

export const CATALOGUE: GuideEntry[] = [...pages, ...actions, ...intents];
