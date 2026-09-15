/** Tunable values shared across the client. */

/** The largest page the API will serve; asking for more is clamped anyway. */
export const MAX_PAGE_SIZE = 200;

/** How deep the hierarchy tree indents before it stops. */
export const MAX_TREE_DEPTH = 4;

/** Height of one timeline row, in pixels. Bars and arrows both key off it. */
export const PLAN_ROW_HEIGHT = 34;

/** The range an unscheduled issue is given when it is first put on the plan. */
export const PLAN_DEFAULT_SPAN_DAYS = 6;

/** How wide the issue column beside the timeline is. */
export const PLAN_SIDEBAR_WIDTH = 320;

/** How far each level of the hierarchy indents in the issue column. */
export const PLAN_INDENT_PER_LEVEL = 14;

/** Height of each of the two header rows above the timeline. */
export const PLAN_HEADER_ROW_HEIGHT = 23;

/** How long the rarely-changing metadata lists stay fresh before a refetch. */
export const META_STALE_MS = 5 * 60_000;

/** Height of the sprint band drawn across the top of the timeline. */
export const PLAN_SPRINT_BAND_HEIGHT = 26;

/** The densest the fitted zoom goes before the calendar scrolls instead. */
export const PLAN_MIN_PX_PER_DAY = 3;

/** Below this density a week is too narrow to label, so months are labelled. */
export const PLAN_WEEK_LABEL_MIN_PX_PER_DAY = 6;

/** A header column narrower than this shows its line but not its label. */
export const PLAN_MIN_TICK_LABEL_PX = 40;

/** The largest file an issue takes, matching the API's limit. */
export const ATTACHMENT_MAX_BYTES = 25 * 1024 * 1024;

/** Size of a status on the workflow canvas, in pixels. */
export const WORKFLOW_NODE_WIDTH = 240;
export const WORKFLOW_NODE_HEIGHT = 72;

/** Statuses snap to this grid when they are dropped, so rows line up. */
export const WORKFLOW_GRID = 8;

/** Where the automatic layout puts the first column and row of statuses. */
export const WORKFLOW_LAYOUT_ORIGIN_X = 32;
export const WORKFLOW_LAYOUT_ORIGIN_Y = 40;

/** How far apart the automatic layout puts columns (one per category) and rows. */
export const WORKFLOW_COLUMN_GAP = 352;
export const WORKFLOW_ROW_GAP = 104;

/** Where the "any status" source of global transitions is drawn: below the
 * first rows, so its arrows climb to their targets instead of cutting through
 * the row. */
export const WORKFLOW_ANYWHERE_X = WORKFLOW_LAYOUT_ORIGIN_X;
export const WORKFLOW_ANYWHERE_Y = WORKFLOW_LAYOUT_ORIGIN_Y + 2 * WORKFLOW_ROW_GAP;

/** How far a transition's curve bows out, so two moves between the same pair stay apart. */
export const WORKFLOW_EDGE_BEND = 64;

/** The canvas is at least this big, and grows with what is drawn on it. */
export const WORKFLOW_CANVAS_MIN_WIDTH = 900;
export const WORKFLOW_CANVAS_MIN_HEIGHT = 520;

/** Room left beyond the furthest status so there is somewhere to drag to. */
export const WORKFLOW_CANVAS_MARGIN = 160;

/** A pointer that travels less than this is a click. A drag is measured from
 * where it crosses this line, so anything bigger takes that much off every drag. */
export const WORKFLOW_DRAG_THRESHOLD_PX = 1;

/** A transition dropped within this distance of a status's centre lands on it. */
export const WORKFLOW_CONNECT_RADIUS = 96;

/** The ring a transition is dragged out of, drawn on a status's right edge. */
export const WORKFLOW_HANDLE_SIZE = 16;

/** The round badge on a status card that carries its category's glyph. */
export const WORKFLOW_BADGE_SIZE = 36;

/** Height of the band of milestone flags drawn above the timeline. */
export const PLAN_MILESTONE_BAND_HEIGHT = 24;

/** About how wide a milestone flag is; one this close to the right edge is drawn to the left of its line. */
export const PLAN_MILESTONE_FLAG_WIDTH = 160;

/** How much of a commit id is shown; seven characters is what git itself abbreviates to. */
export const GIT_SHORT_SHA_LENGTH = 7;

/** A week loaded to this share of its capacity or more reads as full rather than under. */
export const PLAN_LOAD_FULL_RATIO = 0.85;

/** Space left above and below a load bar inside its row. */
export const PLAN_LOAD_CELL_INSET = 3;

/** A pointer that travels less than this is a click, not a drag, whether across empty calendar or down the rows. */
export const PLAN_DRAG_THRESHOLD_PX = 8;

/** The summary box that follows a drag to create is at least this wide, whatever the drag was. */
export const PLAN_DRAFT_INPUT_MIN_PX = 160;

/** How many days a done ticket stays on the plan before it is cut, until the reader says otherwise. */
export const PLAN_DEFAULT_CLOSED_FOR_DAYS = 14;

/** The dot at the end of a bar that a dependency is dragged out of, in pixels across. */
export const PLAN_LINK_HANDLE_PX = 10;

/** A ticket's box on the dependency graph, in pixels. */
export const GRAPH_NODE_WIDTH = 200;
export const GRAPH_NODE_HEIGHT = 40;

/** The space between columns of the graph, which the arrows cross. */
export const GRAPH_COLUMN_GAP = 96;

/** The space between boxes in one column, and between boxes in the grid of unlinked tickets. */
export const GRAPH_ROW_GAP = 16;

/** Where the first box sits, from the canvas's top left. */
export const GRAPH_ORIGIN = 24;

/** The gap above the grid of tickets that wait on nothing, with its caption. */
export const GRAPH_SECTION_GAP = 56;

/** How far an arrow bows out when two run between the same boxes. */
export const GRAPH_EDGE_BEND = 32;

/** The grid of unlinked tickets is never narrower than this many boxes. */
export const GRAPH_MIN_GRID_COLUMNS = 2;

/** How many results the search page shows of what a query matched. */
export const SEARCH_PAGE_SIZE = 100;

/** Control heights: sm for dense rows, md everywhere, lg for the search field and the palette. */
export const CONTROL_HEIGHT_SM = 28;
export const CONTROL_HEIGHT_MD = 32;
export const CONTROL_HEIGHT_LG = 36;

/** Table rows and the denser lists inside panels. */
export const ROW_HEIGHT_TABLE = 36;
export const ROW_HEIGHT_DENSE = 32;

/** The sidebar, open and as the icon rail it becomes on narrow screens. */
export const SIDEBAR_WIDTH = 232;
export const SIDEBAR_RAIL_WIDTH = 48;

/** Below this viewport width the sidebar starts as a rail. */
export const SIDEBAR_RAIL_BELOW_PX = 1024;

/** How wide the issue drawer opens from the right, on a screen too narrow to dock it. */
export const DRAWER_WIDTH = 704;

/** The docked issue panel: its column width, and the viewport width from which it docks. */
export const ISSUE_PANEL_WIDTH = 560;
export const ISSUE_PANEL_DOCK_MIN_PX = 1440;

/** The windows a dashboard's filter tile and a line chart offer, in days. */
export const DASHBOARD_FILTER_WINDOWS = [7, 30, 90, 365] as const;

/** How far the windowed widgets look back, for the ones that look back at all. */
export const DASHBOARD_WINDOWS = [7, 30, 90] as const;

/** A chart shows this many groups and folds the rest into Other; the palette has this many colours. */
export const CHART_MAX_GROUPS = 6;

/** What a windowed widget looks back over until somebody picks another window. */
export const DASHBOARD_DEFAULT_WINDOW_DAYS = 30;

/** How far a line chart goes back until somebody picks another window; a quarter reads as a trend. */
export const CHART_DEFAULT_SINCE_DAYS = 90;

/** A dashboard is left open, so its numbers follow the work at this interval. */
export const DASHBOARD_REFETCH_MS = 60_000;

/** How many of a template's widgets its card names before it trails off. */
export const TEMPLATE_SHAPE_MAX_KINDS = 4;

/** Above this many columns the figures above them collide, so only the bars are drawn. */
export const CHART_VALUE_LABEL_MAX_COLUMNS = 4;

/** About this many labels fit under a chart's x axis; the rest of the ticks go unnamed. */
export const CHART_AXIS_LABELS = 6;

/** How long a toast stays, and how many stack before the oldest goes. */
export const TOAST_MS = 6000;
export const TOAST_MAX = 3;

/** A skeleton waits this long before showing, so a fast load never flashes. */
export const SKELETON_DELAY_MS = 150;

/** A tooltip waits this long, so a pointer passing over shows nothing. */
export const TOOLTIP_DELAY_MS = 400;

/** The icon grid and stroke: every glyph is drawn to these two numbers. */
export const ICON_SIZE_PX = 16;
export const ICON_STROKE = 1.5;

/** How many results of each kind the command palette shows. */
export const PALETTE_RESULTS = 8;

/** The largest profile picture the API takes, matching its limit. */
export const AVATAR_MAX_BYTES = 2 * 1024 * 1024;

/** How many past queries the search page offers again. */
export const RECENT_QUERIES_MAX = 6;

/** How many words and how many issues the search bar offers under itself: a glance, not a page. */
export const SUGGEST_RESULTS = 6;

/** The search bar asks for suggestions from the first character; an empty box shows what was searched before. */
export const SUGGEST_MIN_CHARS = 1;

/** The one token scope the api enforces: such a token reads and never writes. */
export const READ_ONLY_SCOPE = "read";

/** How long a new token lasts unless somebody says otherwise, and the choices offered. */
export const TOKEN_DEFAULT_DAYS = 90;
export const TOKEN_DAY_CHOICES = [30, 90, 365];

/** A question's words that hit an answer's title, keywords or sentence score this much each. */
export const GUIDE_WEIGHT_TITLE = 3;
export const GUIDE_WEIGHT_KEYWORD = 2;
export const GUIDE_WEIGHT_SENTENCE = 1;
/** Below this score per word the guide says it has no answer rather than guessing. */
export const GUIDE_MIN_SCORE = 1;
/** How many answers a question gets at most. */
export const GUIDE_MAX_ANSWERS = 4;
/** A question is one when it has this many words or ends with a question mark. */
export const GUIDE_MIN_WORDS = 2;
/** How far a callout sits from the element it points at. */
export const SPOTLIGHT_GAP_PX = 8;
/** The callout's width, the room it reserves below the element, and the ring around the element. */
export const SPOTLIGHT_WIDTH_PX = 320;
export const SPOTLIGHT_HEIGHT_PX = 120;
export const SPOTLIGHT_RING_PX = 4;
/** How long the callout waits for its element to appear after a navigation. */
export const SPOTLIGHT_WAIT_MS = 3000;

// The inbox: one page of it, how often the bell asks for its count, and the
// mention picker's thresholds.
export const INBOX_PAGE_SIZE = 50;
export const UNREAD_POLL_MS = 30_000;
export const MENTION_MIN_CHARS = 1;
export const MENTION_MAX_SUGGESTIONS = 6;

// Rules and webhooks: how many log rows a drawer shows.
export const RULE_RUNS_PAGE_SIZE = 50;
export const WEBHOOK_DELIVERIES_PAGE_SIZE = 50;

// Many issues at once, and files in and out.
export const BULK_MAX_SELECTION = 200;
export const CSV_PREVIEW_ROWS = 5;
export const EXPORT_COLUMNS = ["key", "summary", "type", "status", "statusCategory", "priority", "assignee", "reporter", "labels", "sprint", "milestone", "fixVersions", "components", "estimate", "created", "updated", "resolved", "due", "start", "parent"] as const;
export const EXPORT_DEFAULT_COLUMNS = ["key", "summary", "type", "status", "priority", "assignee", "created"];

// The one-click rating a resolution mail asks for.
export const CSAT_SCORES = [1, 2, 3, 4, 5] as const;

/** Rows the audit log shows per page; the export has the rest. */
export const AUDIT_PAGE_SIZE = 50;
/** Items a calendar day draws before folding the rest into "+n more". */
export const CALENDAR_MAX_ITEMS_PER_DAY = 4;

/** How long a dependency arrow stays hot after the pointer leaves it, so the control on it can be reached. */
export const EDGE_LINGER_MS = 250;

/** How many comments or history entries an issue page shows before "Show all". */
export const ACTIVITY_FOLD = 12;

/** Lines a description box shows before it scrolls, in the create form. */
export const DESCRIPTION_ROWS = 4;

/** Placeholder rows the create dialog shows while it learns the arrangement. */
export const DIALOG_SKELETON_LINES = 3;

/** The largest file a theme takes, matching the API's limit. */
export const THEME_ASSET_MAX_BYTES = 2 * 1024 * 1024;
/** The most extra CSS a theme carries, matching the API's limit. */
export const THEME_CSS_MAX_BYTES = 32 * 1024;
/** How long a draft settles before the live preview is recompiled. */
export const THEME_PREVIEW_DEBOUNCE_MS = 150;
/** The size an icon is drawn at in the theme editor's grid. */
export const THEME_ICON_PREVIEW_PX = 20;
/** Lines the extra CSS box shows before it scrolls. */
export const THEME_CSS_ROWS = 12;
/** The largest radius a theme may set, matching the API's limit. */
export const THEME_MAX_RADIUS = 32;
/** The furthest a cursor's point may sit from its picture's corner, matching the API's limit. */
export const THEME_MAX_HOTSPOT = 128;
/** The size a cursor picture is best drawn at; larger ones are refused by some browsers. */
export const THEME_CURSOR_PX = 32;
/** Bytes in a kilobyte, for the sizes the editor prints. */
export const KILOBYTE = 1024;
