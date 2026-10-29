/** The strings of the guide, which answers where something is, and the assistant's answers. */
export const guideEn = {
  // What each of a project's pages is for, by its path under the project.
  projectPages: {
    "": "Every issue of the project as a table, with filters and a query.",
    "/board": "The project's boards: columns, swimlanes and cards you drag between statuses.",
    "/sprints": "Sprints planned, running and done, with what each carried.",
    "/plan": "The project as a timeline: bars, dependencies and what is late.",
    "/calendar": "A month of the project's dated work, sprints, milestones and versions; drag an issue to move its dates.",
    "/resources": "The work per team or person per week, in hours, against the hours they have after holidays and absences.",
    "/milestones": "Milestones with their due days and how far each has got.",
    "/dashboard": "Dashboards of widgets and charts over the project's own numbers.",
    "/hierarchy": "The project as a tree: epics, their children and how far each has come.",
    "/queues": "The desk's queues of requests waiting on an agent.",
    "/service-desk": "How requests reach the desk: request types, the portal and the mailbox.",
    "/teams": "The project's teams, their members and their capacity.",
    "/workflows": "Which workflow each issue type follows here, drawn as a graph.",
    "/issue-types": "Which issue types this project allows, and in what order they are offered.",
    "/fields": "The custom fields this project records beyond the standard columns.",
    "/repositories": "The git repositories connected to the project and their webhooks.",
    "/releases": "What the project ships: each version with how close it is, released or not, and its notes of finished work.",
    "/components": "The project's parts, each with a lead and, when it says so, who new work in it goes to.",
    "/import": "A CSV file becomes issues: choose it, map its columns, try it dry, then import.",
    "/automation": "The project's rules: when something happens, check it, do things; each rule keeps a log of its runs.",
    "/settings": "The project's name, its lead and archiving it.",
  },
  projectPageFallback: (label: string) => `The project's ${label.toLowerCase()} page.`,
  // Each answer's title and the sentence its callout says, by the answer's id.
  entries: {
    "page:home": { title: "Home", sentence: "What is yours across every project: assigned to you, watched, recent." },
    "page:projects": { title: "Projects", sentence: "Every project you can see, and where a new one starts from a template." },
    "page:search": { title: "Search", sentence: "Search every project with a query; the help button says what a query can say." },
    "page:filters": { title: "Saved searches", sentence: "Every saved search you may see: yours, shared, starred; each can be mailed to you daily or weekly." },
    "page:inbox": {
      title: "Notifications",
      sentence:
        "What you were told: work assigned to you, mentions, moves on issues you watch; the bell in the rail counts what is unread and opens the latest.",
    },
    "page:settings": { title: "Settings", sentence: "Yours: profile, notification settings, themes and tokens." },
    "page:admin": { title: "Admin", sentence: "Everyone's: installation, access, users, workflows, labels, fields, webhooks and the audit log." },
    "page:profile": {
      title: "Your profile",
      sentence: "Your name, your picture, your time zone and how dates are written for you; the avatar's menu at the foot of the rail opens it.",
    },
    "page:tokens": { title: "Access tokens", sentence: "Tokens for scripts, CI jobs and assistants; one can be made to read and never write." },
    "page:installation": {
      title: "Installation",
      sentence: "What everybody sees the installation called, at the head of the sidebar; administrators rename it here.",
    },
    "page:access": { title: "Access", sentence: "Who holds which role, the groups, invitations and the identity provider." },
    "page:org-workflows": { title: "Workflows", sentence: "The workflows every project follows and the schemes that hand them to issue types." },
    "page:org-automation": { title: "Automation across projects", sentence: "Rules that watch every project: when something happens, check it, do things." },
    "page:webhooks": { title: "Webhooks", sentence: "Where events are posted, signed with a secret shown once, with a log of every try and a retry." },
    "page:holidays": {
      title: "Holidays",
      sentence: "Calendars of days off, imported from an .ics file or typed in; everybody keeps the default until given another under Users.",
    },
    "page:labels": { title: "Labels", sentence: "Words shared by every project, with how often each is used." },
    "action:new-issue": { title: "Create an issue", sentence: "The New issue button in the rail files one in the current project; Ctrl+K reaches it too." },
    "action:share-dashboard": {
      title: "Share a dashboard",
      sentence: "Share on a dashboard makes a link that opens it without a sign-in, until you revoke it.",
    },
    "action:export-pdf": { title: "Export a dashboard as a PDF", sentence: "Export as PDF on a dashboard hands back a file printed from the shared page." },
    "action:dashboard-template": {
      title: "Start a dashboard from a template",
      sentence: "New dashboard offers the built-in templates and the ones saved for everyone; arranging one offers Save as a template.",
    },
    "action:milestone-dashboard": {
      title: "A dashboard for a milestone",
      sentence: "Dashboard on a milestone's card makes one about that milestone from a template.",
    },
    "action:filter-dashboard": { title: "Filter a dashboard", sentence: "A filter tile narrows every widget to a team, a window, issue types or a query." },
    "action:map-workflow": {
      title: "Give an issue type its own workflow",
      sentence: "On the project's Workflow page a project administrator decides which workflow each type follows.",
    },
    "action:design-workflow": {
      title: "Design a workflow",
      sentence: "The designer draws statuses and transitions on a canvas; the Workflows page in Admin opens it.",
    },
    "action:create-token": {
      title: "Create an access token",
      sentence: "Name it on the Access tokens page; tick Can only read for a token that changes nothing.",
    },
    "action:notification-settings": {
      title: "Choose how you are told",
      sentence: "The Notifications grid on your profile turns each reason on or off for Notifications here and for mail, and bundles mail hourly or daily.",
    },
    "action:mention": { title: "Mention somebody in a comment", sentence: "Type an at sign and a name in a comment; the person is made a watcher and told." },
    "action:automation-rule": {
      title: "Make a rule",
      sentence: "New rule on the project's Automation page: a trigger, conditions, and the things to do; the run log says why each run did what it did.",
    },
    "action:save-search": {
      title: "Save a search",
      sentence: "Save this search under the query on the search page names it; a saved search can be shared, starred and mailed.",
    },
    "action:export-csv": {
      title: "Export issues as CSV",
      sentence:
        "Export on the search page writes the issues a query matches to a file with the columns you tick; an issue's More button exports that issue, ticked rows export from the bar above them, and a project's list exports what it shows.",
    },
    "action:bulk": {
      title: "Change many issues at once",
      sentence: "Tick rows on the search page or the issues list and the bar above them changes all of them; what could not be changed is listed with why.",
    },
    "action:clone": { title: "Clone a ticket", sentence: "The More button on an issue's head copies it, with its links and subtasks on request." },
    "action:move": {
      title: "Move work elsewhere",
      sentence: "The More button on an issue's head moves it; it keeps its history and its old address still finds it.",
    },
    "action:knowledge-base": {
      title: "Write down an answer once",
      sentence: "Articles on the service desk page are offered to a customer before they raise a request; publish one when it is ready.",
    },
    "action:canned": {
      title: "Reply with a canned response",
      sentence: "Responses kept on the service desk page are offered in a request's reply box, with the customer's name and the key filled in.",
    },
    "action:business-hours": {
      title: "Count business hours only",
      sentence:
        "A desk's calendar on the service desk page sets its hours, time zone, the holiday calendar it follows and its own days off; each goal chooses whether the weekend counts.",
    },
    "action:desk-companies": {
      title: "Let colleagues see each other's requests",
      sentence:
        "Companies on the service desk page group a desk's customers; whoever raises a request may share it with their company, whose members read it, reply and attach, and never see notes.",
    },
    "action:desk-door": {
      title: "Let people in without a code",
      sentence:
        "The door on the service desk page has the address to share and a switch for the code by mail; off, an address is taken on trust for this desk alone.",
    },
    "action:csat": {
      title: "How customers rated the desk",
      sentence: "The resolution mail asks for a score; the queue shows it beside each request and a CSAT widget on the dashboard adds them up.",
    },
    "action:audit": {
      title: "Who did what in Armature",
      sentence: "The audit log in Admin lists roles granted, groups changed, projects made or archived, tokens and sign-ins, for a year.",
    },
    "action:org-field": {
      title: "A field every project has",
      sentence: "Fields in Admin are on every project; a project's own field is promoted there from its Fields page and same-named fields fold into one.",
    },
    "action:status-update": {
      title: "Say how the project is doing",
      sentence: "Post an update on the project's issues page: on track, at risk or off track, with a note; the projects list shows the latest.",
    },
    "action:theme": { title: "Change the theme", sentence: "The button at the foot of the rail cycles between following the system, light and dark." },
    "action:demo-project": {
      title: "Add the demo project",
      sentence:
        "The Installation page fills the installation with a made-up plant: a production line and its maintenance, with issues, sprints and boards to show.",
    },
    "action:invite": { title: "Invite somebody", sentence: "Access is where invitations are sent and roles are granted." },
    "action:query-help": {
      title: "What a query can say",
      sentence: "The help button beside the query field lists the fields, operators and functions of a query.",
    },
    "action:issue-beside": {
      title: "Open an issue beside the list",
      sentence: "Clicking a row on the issues page or a card on the board opens it in a panel; the arrow keys walk the list.",
    },
    "action:palette": {
      title: "Reach anything with Ctrl+K",
      sentence: "The palette jumps to an issue by its key, searches summaries and reaches every page and action.",
    },
    "intent:blocked": { title: "What is blocked", sentence: "The plan's dependencies view draws what waits on what." },
    "intent:mine": { title: "My open issues", sentence: "Everything assigned to you that is not done." },
    "intent:reported": { title: "Issues I reported", sentence: "Everything you filed, whatever its state." },
    "intent:due-week": { title: "Due this week", sentence: "Open issues due by the end of this week." },
    "intent:overdue": { title: "Overdue issues", sentence: "Open issues whose due day has passed." },
    "intent:today": { title: "Changed today", sentence: "Issues somebody touched since midnight." },
    "intent:unassigned": { title: "Unassigned issues", sentence: "Open issues nobody holds." },
    "intent:urgent": { title: "Highest priority", sentence: "Open issues at the two highest priorities." },
    "intent:bugs": { title: "Open bugs", sentence: "Every bug that is not done." },
    "intent:in-progress": { title: "In progress now", sentence: "What is being worked on right now." },
    "intent:done-week": { title: "Done this week", sentence: "What was resolved since the week began." },
    "intent:no-estimate": { title: "Without an estimate", sentence: "Open issues nobody has sized." },
  },
  ask: {
    asking: "Asking...",
    goThere: "Go there",
    done: "Done",
    confirm: "Confirm",
  },
};
