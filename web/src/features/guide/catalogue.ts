import type { Feature } from "@/api/projects";
import { projectSetupPages, projectWorkPages } from "@/features/shell/Sidebar";

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

const PROJECT_PAGE_WORDS: Record<string, { sentence: string; keywords: string[]; target?: string }> = {
  "": { sentence: "Every issue of the project as a table, with filters and a query.", keywords: ["issue", "ticket", "list", "table", "filter", "open"] },
  "/board": { sentence: "The project's boards: columns, swimlanes and cards you drag between statuses.", keywords: ["board", "kanban", "scrum", "column", "card", "swimlane", "drag"] },
  "/sprints": { sentence: "Sprints planned, running and done, with what each carried.", keywords: ["sprint", "iteration", "velocity", "commit", "backlog"] },
  "/plan": { sentence: "The project as a timeline: bars, dependencies and what is late.", keywords: ["plan", "gantt", "timeline", "schedule", "dependency", "late", "roadmap"] },
  "/calendar": { sentence: "A month of the project's dated work, sprints, milestones and versions; drag an issue to move its dates.", keywords: ["calendar", "month", "week", "day", "date", "due", "agenda", "when"] },
  "/milestones": { sentence: "Milestones with their due days and how far each has got.", keywords: ["milestone", "release", "due", "overdue", "progress", "deadline"] },
  "/dashboard": { sentence: "Dashboards of widgets and charts over the project's own numbers.", keywords: ["dashboard", "chart", "widget", "report", "burndown", "throughput", "graph", "statistic", "metric"] },
  "/hierarchy": { sentence: "The project as a tree: epics, their children and how far each has come.", keywords: ["hierarchy", "epic", "tree", "parent", "child", "breakdown"] },
  "/queues": { sentence: "The desk's queues of requests waiting on an agent.", keywords: ["queue", "request", "desk", "waiting", "sla"] },
  "/service-desk": { sentence: "How requests reach the desk: request types, the portal and the mailbox.", keywords: ["desk", "portal", "request", "mail", "customer", "support"] },
  "/teams": { sentence: "The project's teams, their members and their capacity.", keywords: ["team", "member", "capacity", "people"] },
  "/workflows": { sentence: "Which workflow each issue type follows here, drawn as a graph.", keywords: ["workflow", "status", "transition", "scheme", "type", "map", "graph", "flow"], target: "workflow-decide" },
  "/fields": { sentence: "The custom fields this project records beyond the standard columns.", keywords: ["field", "custom", "column", "attribute", "property"] },
  "/repositories": { sentence: "The git repositories connected to the project and their webhooks.", keywords: ["repository", "git", "branch", "commit", "webhook", "gitea", "github", "gitlab", "pull"] },
  "/releases": { sentence: "What the project ships: each version with how close it is, released or not, and its notes of finished work.", keywords: ["release", "version", "ship", "fix version", "changelog", "notes", "release notes", "deploy"], target: "new-version" },
  "/components": { sentence: "The project's parts, each with a lead and, when it says so, who new work in it goes to.", keywords: ["component", "part", "module", "area", "owner", "lead", "default assignee"], target: "new-component" },
  "/import": { sentence: "A CSV file becomes issues: choose it, map its columns, try it dry, then import.", keywords: ["import", "csv", "file", "upload", "spreadsheet", "excel", "migrate", "bulk create"], target: "import" },
  "/automation": { sentence: "The project's rules: when something happens, check it, do things; each rule keeps a log of its runs.", keywords: ["automation", "rule", "automate", "trigger", "action", "schedule", "run", "log"], target: "new-rule" },
  "/settings": { sentence: "The project's name, its lead and archiving it.", keywords: ["project", "setting", "rename", "lead", "archive", "delete"] },
};

function projectPage(path: string, label: string, feature?: Feature): GuideEntry {
  const words = PROJECT_PAGE_WORDS[path] ?? { sentence: `The project's ${label.toLowerCase()} page.`, keywords: [] };
  return {
    id: `page:${path || "/issues"}`,
    title: label,
    sentence: words.sentence,
    keywords: [label.toLowerCase(), ...words.keywords],
    needs: "project",
    feature,
    target: words.target,
    to: (ctx) => ({ to: `/projects/$projectKey${path}`, params: { projectKey: ctx.projectKey ?? "" } }),
  };
}

const pages: GuideEntry[] = [
  { id: "page:home", title: "Home", sentence: "What is yours across every project: assigned to you, watched, recent.", keywords: ["home", "start", "overview", "mine", "recent"], to: () => ({ to: "/" }) },
  { id: "page:projects", title: "Projects", sentence: "Every project you can see, and where a new one starts from a template.", keywords: ["project", "new", "create", "template", "list"], to: () => ({ to: "/projects" }) },
  { id: "page:search", title: "Search", sentence: "Search every project with a query; the help button says what a query can say.", keywords: ["search", "query", "nql", "find", "filter", "syntax", "help", "language"], target: "query", to: () => ({ to: "/search", search: { q: "" } }) },
  ...projectWorkPages.map((page) => projectPage(page.path, page.label, page.feature)),
  ...projectSetupPages.map((page) => projectPage(page.path, page.label, page.feature)),
  { id: "page:filters", title: "Saved searches", sentence: "Every saved search you may see: yours, shared, starred; each can be mailed to you daily or weekly.", keywords: ["saved", "search", "filter", "query", "subscription", "subscribe", "star", "starred", "mail"], to: () => ({ to: "/filters" }) },
  { id: "page:inbox", title: "Inbox: your notifications", sentence: "What you were told: work assigned to you, mentions, moves on issues you watch; the bell in the sidebar's foot counts what is unread.", keywords: ["inbox", "notification", "bell", "unread", "told", "mention", "mentioned"], target: "inbox", to: () => ({ to: "/inbox" }) },
  { id: "page:settings", title: "Settings", sentence: "Yours and the organization's: profile, tokens, access, workflows, labels.", keywords: ["setting", "preference", "configure", "admin"], to: () => ({ to: "/settings" }) },
  { id: "page:profile", title: "Profile", sentence: "Your name, your picture, your time zone and how dates are written for you.", keywords: ["profile", "name", "picture", "avatar", "photo", "timezone", "zone", "language", "locale", "date", "account", "me"], to: () => ({ to: "/settings/profile" }) },
  { id: "page:tokens", title: "API tokens", sentence: "Tokens for scripts, CI jobs and assistants; one can be made to read and never write.", keywords: ["token", "api", "key", "pat", "script", "ci", "mcp", "assistant", "bearer", "read-only", "readonly", "secret"], target: "token-form", to: () => ({ to: "/settings/tokens" }) },
  { id: "page:access", title: "Access", sentence: "Who holds which role, the groups, invitations and the identity provider.", keywords: ["access", "role", "permission", "group", "invite", "invitation", "member", "user", "sso", "oidc", "keycloak", "login", "right", "administrator"], needs: "admin", to: () => ({ to: "/settings/access" }) },
  { id: "page:org-workflows", title: "Workflows", sentence: "The organization's workflows and the schemes that hand them to issue types.", keywords: ["workflow", "scheme", "status", "transition", "design", "designer", "canvas", "organization"], needs: "admin", to: () => ({ to: "/settings/workflows" }) },
  { id: "page:org-automation", title: "Automation across projects", sentence: "The organization's rules, which watch every project: when something happens, check it, do things.", keywords: ["automation", "rule", "automate", "trigger", "when", "then", "organization"], needs: "admin", to: () => ({ to: "/settings/automation" }) },
  { id: "page:webhooks", title: "Webhooks", sentence: "Where the organization's events are posted, signed with a secret shown once, with a log of every try and a retry.", keywords: ["webhook", "hook", "post", "endpoint", "slack", "teams", "chat", "integration", "event", "signature", "secret", "delivery"], needs: "admin", target: "new-webhook", to: () => ({ to: "/settings/webhooks" }) },
  { id: "page:labels", title: "Labels", sentence: "Words shared by every project, with how often each is used.", keywords: ["label", "tag", "word"], to: () => ({ to: "/settings/labels" }) },
];

const actions: GuideEntry[] = [
  { id: "action:new-issue", title: "Create an issue", sentence: "The New issue button in the sidebar files one in the current project; Ctrl+K reaches it too.", keywords: ["create", "new", "file", "add", "issue", "ticket", "bug", "task", "story", "report"], target: "new-issue", to: (ctx) => (ctx.projectKey ? { to: "/projects/$projectKey", params: { projectKey: ctx.projectKey } } : { to: "/projects" }) },
  { id: "action:share-dashboard", title: "Share a dashboard", sentence: "Share on a dashboard makes a link that opens it without a sign-in, until you revoke it.", keywords: ["share", "link", "public", "external", "monitor", "screen", "wall", "revoke", "dashboard"], needs: "project", target: "share", to: (ctx) => ({ to: "/projects/$projectKey/dashboard", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:export-pdf", title: "Export a dashboard as a PDF", sentence: "Export as PDF on a dashboard hands back a file printed from the shared page.", keywords: ["export", "pdf", "print", "download", "file", "dashboard"], needs: "project", target: "export-pdf", to: (ctx) => ({ to: "/projects/$projectKey/dashboard", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:dashboard-template", title: "Start a dashboard from a template", sentence: "New dashboard offers the built-in templates and the organization's own; arranging one offers Save as a template.", keywords: ["template", "dashboard", "save", "reuse", "copy", "new"], needs: "project", target: "add-dashboard", to: (ctx) => ({ to: "/projects/$projectKey/dashboard", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:milestone-dashboard", title: "A dashboard for a milestone", sentence: "Dashboard on a milestone's card makes one about that milestone from a template.", keywords: ["milestone", "dashboard", "progress", "release", "track"], needs: "project", target: "milestone-dashboard", to: (ctx) => ({ to: "/projects/$projectKey/milestones", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:filter-dashboard", title: "Filter a dashboard", sentence: "A filter tile narrows every widget to a team, a window, issue types or a query.", keywords: ["filter", "narrow", "team", "type", "window", "days", "dashboard", "tile"], needs: "project", target: "arrange", to: (ctx) => ({ to: "/projects/$projectKey/dashboard", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:map-workflow", title: "Give an issue type its own workflow", sentence: "On the project's Workflow page a project administrator decides which workflow each type follows.", keywords: ["workflow", "type", "map", "assign", "decide", "change", "different", "own", "bug", "status"], needs: "project", target: "workflow-decide", to: (ctx) => ({ to: "/projects/$projectKey/workflows", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:design-workflow", title: "Design a workflow", sentence: "The designer draws statuses and transitions on a canvas; the organization's workflows page opens it.", keywords: ["design", "draw", "workflow", "status", "transition", "canvas", "new", "create", "edit"], needs: "admin", to: () => ({ to: "/settings/workflows/new" }) },
  { id: "action:create-token", title: "Create an API token", sentence: "Name it on the tokens page; tick Can only read for a token that changes nothing.", keywords: ["token", "api", "key", "create", "script", "ci", "mcp", "assistant", "read-only", "readonly", "claude"], target: "token-form", to: () => ({ to: "/settings/tokens" }) },
  { id: "action:notification-settings", title: "Choose how you are told", sentence: "The Notifications grid on your profile turns each reason on or off for the inbox and for mail, and bundles mail hourly or daily.", keywords: ["mail", "digest", "bundle", "quiet", "mute", "off", "preference", "notification", "settings"], target: "notification-settings", to: () => ({ to: "/settings/profile" }) },
  { id: "action:mention", title: "Mention somebody in a comment", sentence: "Type an at sign and a name in a comment; the person is made a watcher and told.", keywords: ["mention", "@", "name", "ping", "tag", "notify", "comment", "watcher", "tell"], needs: "project", to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:automation-rule", title: "Make a rule", sentence: "New rule on the project's Automation page: a trigger, conditions, and the things to do; the run log says why each run did what it did.", keywords: ["automation", "rule", "automate", "trigger", "when", "then", "label automatically", "assign automatically", "schedule", "cron", "workflow automation", "run log"], needs: "project", target: "new-rule", to: (ctx) => ({ to: "/projects/$projectKey/automation", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:save-search", title: "Save a search", sentence: "Save this search under the query on the search page names it; a saved search can be shared, starred and mailed.", keywords: ["save", "search", "filter", "query", "named", "share", "remember"], target: "save-search", to: () => ({ to: "/search", search: { q: "statusCategory != done" } }) },
  { id: "action:export-csv", title: "Export issues as CSV", sentence: "Export on the search page writes the issues a query matches to a file with the columns you tick.", keywords: ["export", "csv", "file", "download", "spreadsheet", "excel", "issues"], target: "export-csv", to: () => ({ to: "/search", search: { q: "" } }) },
  { id: "action:bulk", title: "Change many issues at once", sentence: "Tick rows on the search page or the issues list and the bar above them changes all of them; what could not be changed is listed with why.", keywords: ["bulk", "many", "several", "multiple", "batch", "select", "at once", "mass"], to: () => ({ to: "/search", search: { q: "" } }) },
  { id: "action:clone", title: "Clone a ticket", sentence: "The More button on an issue's head copies it, with its links and subtasks on request.", keywords: ["clone", "copy", "duplicate"], needs: "project", to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:move", title: "Move work elsewhere", sentence: "The More button on an issue's head moves it; it keeps its history and its old address still finds it.", keywords: ["move", "transfer", "relocate", "elsewhere"], needs: "project", to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:knowledge-base", title: "Write down an answer once", sentence: "Articles on the service desk page are offered to a customer before they raise a request; publish one when it is ready.", keywords: ["article", "knowledge", "knowledge base", "kb", "faq", "self service", "help article", "publish"], needs: "project", target: "knowledge-base", to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:canned", title: "Reply with a canned response", sentence: "Responses kept on the service desk page are offered in a request's reply box, with the customer's name and the key filled in.", keywords: ["canned", "canned response", "saved reply", "template reply", "macro", "snippet", "boilerplate"], needs: "project", target: "canned-responses", to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:business-hours", title: "Count business hours only", sentence: "A desk's calendar on the service desk page sets its hours, time zone and days off; each goal chooses whether the weekend counts.", keywords: ["business hours", "calendar", "working hours", "office hours", "weekend", "holiday", "time zone", "timezone", "sla calendar"], needs: "project", target: "business-hours", to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:desk-door", title: "Let people in without a code", sentence: "The door on the service desk page has the address to share and a switch for the code by mail; off, an address is taken on trust for this desk alone.", keywords: ["door", "code", "mail verification", "verify", "verification", "without a code", "no code", "portal address", "desk address", "walk in", "trust"], needs: "project", target: "desk-door", to: (ctx) => ({ to: "/projects/$projectKey/service-desk", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:csat", title: "How customers rated the desk", sentence: "The resolution mail asks for a score; the queue shows it beside each request and a CSAT widget on the dashboard adds them up.", keywords: ["csat", "satisfaction", "rating", "rated", "score", "survey", "feedback", "happy", "nps"], needs: "project", to: (ctx) => ({ to: "/projects/$projectKey/queues", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:audit", title: "Who did what to the organization", sentence: "The audit log in Settings lists roles granted, groups changed, projects made or archived, tokens and sign-ins, for a year.", keywords: ["audit", "log", "history", "who", "granted", "changed", "security", "compliance", "trail", "record"], needs: "admin", to: () => ({ to: "/settings/audit" }) },
  { id: "action:org-field", title: "A field every project has", sentence: "Fields in Settings are the organization's; a project's own field is promoted there from its Fields page and same-named fields fold into one.", keywords: ["organization field", "org field", "global field", "shared field", "every project", "promote", "all projects"], needs: "admin", to: () => ({ to: "/settings/fields" }) },
  { id: "action:status-update", title: "Say how the project is doing", sentence: "Post an update on the project's issues page: on track, at risk or off track, with a note; the projects list shows the latest.", keywords: ["status update", "health", "on track", "at risk", "off track", "how is the project", "progress report", "update"], needs: "project", target: "post-status", to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:theme", title: "Change the theme", sentence: "The button in the sidebar's foot cycles between following the system, light and dark.", keywords: ["theme", "dark", "light", "mode", "colour", "color", "appearance", "night"], target: "theme", to: () => ({ to: "/settings/profile" }) },
  { id: "action:invite", title: "Invite somebody", sentence: "Access is where invitations are sent and roles are granted.", keywords: ["invite", "invitation", "add", "people", "user", "member", "colleague", "join"], needs: "admin", to: () => ({ to: "/settings/access" }) },
  { id: "action:query-help", title: "What a query can say", sentence: "The help button beside the query field lists the fields, operators and functions of a query.", keywords: ["query", "nql", "syntax", "help", "field", "operator", "function", "currentuser", "search", "language", "write"], target: "query", to: () => ({ to: "/search", search: { q: "" } }) },
  { id: "action:issue-beside", title: "Open an issue beside the list", sentence: "Clicking a row on the issues page or a card on the board opens it in a panel; the arrow keys walk the list.", keywords: ["panel", "drawer", "beside", "side", "preview", "quick", "arrow", "keyboard", "open", "issue"], needs: "project", to: (ctx) => ({ to: "/projects/$projectKey", params: { projectKey: ctx.projectKey ?? "" } }) },
  { id: "action:palette", title: "Reach anything with Ctrl+K", sentence: "The palette jumps to an issue by its key, searches summaries and reaches every page and action.", keywords: ["palette", "shortcut", "ctrl", "cmd", "keyboard", "command", "jump", "quick", "fast"], to: () => ({ to: "/" }) },
];

/** A question that is really a query: the words compile to a search. */
function intent(id: string, title: string, sentence: string, keywords: string[], q: string): GuideEntry {
  return { id: `intent:${id}`, title, sentence, keywords, to: () => ({ to: "/search", search: { q } }) };
}

const intents: GuideEntry[] = [
  intent("mine", "My open issues", "Everything assigned to you that is not done.", ["my", "mine", "me", "assigned", "open", "issue", "work", "todo"], "assignee = currentUser() AND statusCategory != done ORDER BY priority DESC"),
  intent("reported", "Issues I reported", "Everything you filed, whatever its state.", ["reported", "filed", "created", "my", "me", "raised"], "reporter = currentUser() ORDER BY created DESC"),
  intent("due-week", "Due this week", "Open issues due by the end of this week.", ["due", "week", "deadline", "soon", "upcoming"], "due <= endOfWeek() AND statusCategory != done ORDER BY due ASC"),
  intent("overdue", "Overdue issues", "Open issues whose due day has passed.", ["overdue", "late", "past", "due", "missed", "behind"], "due < startOfDay() AND statusCategory != done ORDER BY due ASC"),
  intent("today", "Changed today", "Issues somebody touched since midnight.", ["today", "updated", "changed", "recent", "activity", "happened"], "updated >= startOfDay() ORDER BY updated DESC"),
  intent("unassigned", "Unassigned issues", "Open issues nobody holds.", ["unassigned", "nobody", "free", "unowned", "assignee", "empty"], "assignee IS EMPTY AND statusCategory != done"),
  intent("urgent", "Highest priority", "Open issues at the two highest priorities.", ["urgent", "priority", "highest", "high", "critical", "important"], "priority IN (high, highest) AND statusCategory != done ORDER BY priority DESC"),
  intent("bugs", "Open bugs", "Every bug that is not done.", ["bug", "defect", "broken", "open"], "type = Bug AND statusCategory != done ORDER BY priority DESC"),
  intent("in-progress", "In progress now", "What is being worked on right now.", ["progress", "doing", "working", "active", "started", "now"], "statusCategory = in_progress ORDER BY updated DESC"),
  intent("done-week", "Done this week", "What was resolved since the week began.", ["done", "finished", "resolved", "closed", "completed", "week", "shipped"], "resolved >= startOfWeek() ORDER BY resolved DESC"),
  intent("no-estimate", "Without an estimate", "Open issues nobody has sized.", ["estimate", "unestimated", "sized", "points", "missing"], "estimate IS EMPTY AND statusCategory != done"),
  {
    id: "intent:blocked", title: "What is blocked", sentence: "The plan's dependencies view draws what waits on what.", keywords: ["blocked", "blocker", "blocking", "depends", "dependency", "waiting", "stuck"], needs: "project",
    to: (ctx) => ({ to: "/projects/$projectKey/plan", params: { projectKey: ctx.projectKey ?? "" }, search: { view: "dependencies" } }),
  },
];

export const CATALOGUE: GuideEntry[] = [...pages, ...actions, ...intents];
