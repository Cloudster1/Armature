import type { SVGProps } from "react";
import { ICON_SIZE_PX, ICON_STROKE } from "@/config";

// Forty glyphs drawn here on a 16px grid rather than an icon library: one
// weight, one corner radius, and a missing one is drawn in ten minutes.
export type IconProps = Omit<SVGProps<SVGSVGElement>, "children"> & {
  /** Says what the icon means when it stands alone; beside text it is decoration. */
  label?: string;
  size?: number;
};

export const ICON_SIZE = ICON_SIZE_PX;

function makeIcon(name: string, paths: string[]) {
  function Icon({ label, size = ICON_SIZE, className, ...rest }: IconProps) {
    return (
      <svg
        {...rest}
        width={size}
        height={size}
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth={ICON_STROKE}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden={label ? undefined : true}
        role={label ? "img" : undefined}
        className={className}
        data-icon={name}
      >
        {label && <title>{label}</title>}
        {paths.map((d, i) => (
          <path key={i} d={d} />
        ))}
      </svg>
    );
  }
  Icon.displayName = `Icon.${name}`;
  return Icon;
}

export const Icon = {
  Home: makeIcon("home", ["M2.5 7.5 8 3l5.5 4.5", "M4 6.5V13h8V6.5", "M6.5 13V9.5h3V13"]),
  Search: makeIcon("search", ["M7 12A5 5 0 1 0 7 2a5 5 0 0 0 0 10Z", "m10.5 10.5 3 3"]),
  Plus: makeIcon("plus", ["M8 3v10", "M3 8h10"]),
  Issue: makeIcon("issue", ["M3.5 2.5h9v11h-9z", "M6 6h4", "M6 9h4"]),
  Board: makeIcon("board", ["M2.5 3h11v10h-11z", "M6.2 3v10", "M9.8 3v10"]),
  Sprint: makeIcon("sprint", ["M2.5 8a5.5 5.5 0 0 1 9.4-3.9", "M12 2.5v2.6H9.4", "M13.5 8a5.5 5.5 0 0 1-9.4 3.9", "M4 13.5v-2.6h2.6"]),
  Plan: makeIcon("plan", ["M2.5 4h6", "M2.5 8h9", "M2.5 12h5", "M11 11.5l2.5 2"]),
  Milestone: makeIcon("milestone", ["M4 2.5v11", "M4 3h8l-2 2.5 2 2.5H4"]),
  Dashboard: makeIcon("dashboard", ["M2.5 2.5h4.5v6H2.5z", "M9 2.5h4.5v3.5H9z", "M9 8h4.5v5.5H9z", "M2.5 10.5h4.5v3H2.5z"]),
  Hierarchy: makeIcon("hierarchy", ["M6 2.5h4v3H6z", "M2.5 10.5h4v3h-4z", "M9.5 10.5h4v3h-4z", "M8 5.5V8", "M4.5 10.5V8h7v2.5"]),
  Queue: makeIcon("queue", ["M2.5 4h11", "M2.5 8h11", "M2.5 12h7"]),
  Desk: makeIcon("desk", ["M2.5 6.5h11v3h-11z", "M4 9.5v4", "M12 9.5v4", "M8 2.5v4"]),
  Team: makeIcon("team", ["M6 7.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z", "M1.5 13.5a4.5 4.5 0 0 1 9 0", "M10.5 7.5a2 2 0 1 0-1.4-3.4", "M11.5 9.6a4 4 0 0 1 3 3.9"]),
  Workflow: makeIcon("workflow", ["M2.5 4a1.5 1.5 0 1 0 3 0 1.5 1.5 0 0 0-3 0Z", "M10.5 12a1.5 1.5 0 1 0 3 0 1.5 1.5 0 0 0-3 0Z", "M5.5 4H9a2 2 0 0 1 2 2v4", "m9.5 8.5 1.5 1.5 1.5-1.5"]),
  Field: makeIcon("field", ["M2.5 4.5h11v7h-11z", "M5 8h1", "M8 8h3"]),
  Repository: makeIcon("repository", ["M4.5 2.5h8v11h-8a1.5 1.5 0 0 1-1.5-1.5V4a1.5 1.5 0 0 1 1.5-1.5Z", "M4.5 10.5h8", "M7 5.5h3"]),
  Settings: makeIcon("settings", ["M8 10.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z", "M8 1.5v1.8", "M8 12.7v1.8", "M1.5 8h1.8", "M12.7 8h1.8", "m3.4 3.4 1.3 1.3", "m11.3 11.3 1.3 1.3", "m3.4 12.6 1.3-1.3", "m11.3 4.7 1.3-1.3"]),
  Key: makeIcon("key", ["M6 10.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z", "m8.5 9.5 5 5", "m11.5 12.5 1.5-1.5"]),
  Users: makeIcon("users", ["M8 8a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z", "M2.5 14a5.5 5.5 0 0 1 11 0"]),
  User: makeIcon("user", ["M8 8a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z", "M2.5 14a5.5 5.5 0 0 1 11 0"]),
  Tag: makeIcon("tag", ["M2.5 2.5h5l6 6-5 5-6-6z", "M5.5 5.5h.01"]),
  ChevronDown: makeIcon("chevron-down", ["m4 6 4 4 4-4"]),
  ChevronRight: makeIcon("chevron-right", ["m6 4 4 4-4 4"]),
  ChevronLeft: makeIcon("chevron-left", ["m10 4-4 4 4 4"]),
  ChevronUp: makeIcon("chevron-up", ["m4 10 4-4 4 4"]),
  Check: makeIcon("check", ["m3 8.5 3 3 7-7"]),
  Play: makeIcon("play", ["M5.5 3.5v9l7.5-4.5z"]),
  Lines: makeIcon("lines", ["M3.5 5h9", "M3.5 8h9", "M3.5 11h9"]),
  Bold: makeIcon("bold", ["M5 3h4a2.5 2.5 0 0 1 0 5H5z", "M5 8h4.5a2.5 2.5 0 0 1 0 5H5z"]),
  Italic: makeIcon("italic", ["M7 3h5", "M4 13h5", "M9.5 3l-3 10"]),
  Code: makeIcon("code", ["M5.5 5 2.5 8l3 3", "M10.5 5l3 3-3 3", "M9 3.5 7 12.5"]),
  Quote: makeIcon("quote", ["M4 4.5v7", "M7 5h6", "M7 8h6", "M7 11h4"]),
  OrderedList: makeIcon("ordered-list", ["M6.5 4.5h6", "M6.5 8h6", "M6.5 11.5h6", "M2.5 3.5h1v2.5", "M2.5 9h1.5l-1.5 2h1.5"]),
  Heading: makeIcon("heading", ["M3.5 3.5v9", "M9.5 3.5v9", "M3.5 8h6", "M12.5 8.5v4"]),
  X: makeIcon("x", ["m4 4 8 8", "m12 4-8 8"]),
  More: makeIcon("more", ["M3.5 8h.01", "M8 8h.01", "M12.5 8h.01"]),
  Edit: makeIcon("edit", ["m10.5 2.5 3 3-8 8h-3v-3z", "m9 4 3 3"]),
  Trash: makeIcon("trash", ["M2.5 4.5h11", "M5.5 4.5V3h5v1.5", "M4 4.5 4.7 13h6.6l.7-8.5", "M7 7.5v3.5", "M9 7.5v3.5"]),
  Archive: makeIcon("archive", ["M2.5 3h11v3h-11z", "M3.5 6v7.5h9V6", "M6.5 9h3"]),
  Link: makeIcon("link", ["M6.5 9.5 9.5 6.5", "M7 4.5l1.3-1.3a2.8 2.8 0 0 1 4 4L11 8.5", "M9 11.5l-1.3 1.3a2.8 2.8 0 0 1-4-4L5 7.5"]),
  Unlink: makeIcon("unlink", ["M7 4.5l1.3-1.3a2.8 2.8 0 0 1 4 4L11 8.5", "M9 11.5l-1.3 1.3a2.8 2.8 0 0 1-4-4L5 7.5", "m3 3 10 10"]),
  Attach: makeIcon("attach", ["M10.5 5 6 9.5a1.4 1.4 0 0 0 2 2l5-5a2.8 2.8 0 0 0-4-4L4 7.5a4.2 4.2 0 0 0 6 6l3-3"]),
  Clock: makeIcon("clock", ["M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12Z", "M8 4.5V8l2.5 1.5"]),
  Calendar: makeIcon("calendar", ["M2.5 4h11v9.5h-11z", "M2.5 7h11", "M5.5 2.5V5", "M10.5 2.5V5"]),
  Shield: makeIcon("shield", ["M8 2 3 4v4c0 3 2.2 5 5 6 2.8-1 5-3 5-6V4z", "m6 8 1.5 1.5L10.5 6"]),
  Comment: makeIcon("comment", ["M2.5 3h11v7.5H7l-3 2.5v-2.5H2.5z"]),
  Eye: makeIcon("eye", ["M1.5 8s2.5-4.5 6.5-4.5S14.5 8 14.5 8s-2.5 4.5-6.5 4.5S1.5 8 1.5 8Z", "M8 10a2 2 0 1 0 0-4 2 2 0 0 0 0 4Z"]),
  EyeOff: makeIcon("eye-off", ["M2.5 2.5 13.5 13.5", "M6.4 6.5a2 2 0 0 0 2.9 2.8", "M4.3 4.6C2.6 5.8 1.5 8 1.5 8s2.5 4.5 6.5 4.5c1.2 0 2.3-.4 3.2-.9", "M6.8 3.6c.4-.1.8-.1 1.2-.1 4 0 6.5 4.5 6.5 4.5s-.6 1.1-1.7 2.2"]),
  Sun: makeIcon("sun", ["M8 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z", "M8 1.5v1.5", "M8 13v1.5", "M1.5 8H3", "M13 8h1.5", "m3.4 3.4 1 1", "m11.6 11.6 1 1", "m3.4 12.6 1-1", "m11.6 4.4 1-1"]),
  Moon: makeIcon("moon", ["M13 9.5A5.5 5.5 0 0 1 6.5 3a5.5 5.5 0 1 0 6.5 6.5Z"]),
  Monitor: makeIcon("monitor", ["M2.5 3h11v8h-11z", "M6 13.5h4", "M8 11v2.5"]),
  External: makeIcon("external", ["M9 2.5h4.5V7", "M13.5 2.5 7.5 8.5", "M11 9.5v4H2.5V5h4"]),
  Filter: makeIcon("filter", ["M2.5 3h11l-4.5 5.5v4l-2 1V8.5z"]),
  ZoomIn: makeIcon("zoom-in", ["M7 12A5 5 0 1 0 7 2a5 5 0 0 0 0 10Z", "m10.5 10.5 3 3", "M7 5v4", "M5 7h4"]),
  ZoomOut: makeIcon("zoom-out", ["M7 12A5 5 0 1 0 7 2a5 5 0 0 0 0 10Z", "m10.5 10.5 3 3", "M5 7h4"]),
  Warning: makeIcon("warning", ["M8 2.5 14 13H2z", "M8 6.5v3", "M8 11.5h.01"]),
  Info: makeIcon("info", ["M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12Z", "M8 7.5V11", "M8 5.5h.01"]),
  Drag: makeIcon("drag", ["M6 4h.01", "M10 4h.01", "M6 8h.01", "M10 8h.01", "M6 12h.01", "M10 12h.01"]),
  Collapse: makeIcon("collapse", ["M2.5 3h11v10h-11z", "M6 3v10", "m10.5 6.5-1.5 1.5 1.5 1.5"]),
  Expand: makeIcon("expand", ["M2.5 3h11v10h-11z", "M6 3v10", "m9 6.5 1.5 1.5L9 9.5"]),
  Command: makeIcon("command", ["M5 3.5A1.5 1.5 0 0 0 3.5 5v6A1.5 1.5 0 0 0 5 12.5", "M11 3.5A1.5 1.5 0 0 1 12.5 5v6a1.5 1.5 0 0 1-1.5 1.5", "M5 5.5h6v5H5z"]),
  Bell: makeIcon("bell", ["M4 11V7a4 4 0 0 1 8 0v4l1 1.5H3z", "M6.5 13.5a1.5 1.5 0 0 0 3 0"]),
  Upload: makeIcon("upload", ["M8 10.5V3", "m4.5 6.5 3.5-3.5 3.5 3.5", "M2.5 13.5h11"]),
  Download: makeIcon("download", ["M8 3v7.5", "m4.5 7 3.5 3.5L11.5 7", "M2.5 13.5h11"]),
  Share: makeIcon("share", ["M8 9.5V2.5", "m5 5.5 3-3 3 3", "M3.5 8v5h9V8"]),
  Copy: makeIcon("copy", ["M5.5 5.5h8v8h-8z", "M10.5 5.5v-3h-8v8h3"]),
  Mail: makeIcon("mail", ["M2.5 3.5h11v9h-11z", "m2.5 4 5.5 4.5L13.5 4"]),
  Ship: makeIcon("ship", ["M2.5 10.5 8 12.5l5.5-2", "M3.5 10V6.5h9V10", "M8 6.5v-3l2 1.5"]),
  Component: makeIcon("component", ["M8 2.5 13 5.5v5L8 13.5 3 10.5v-5z", "M3 5.5l5 3 5-3", "M8 8.5v5"]),
  Bolt: makeIcon("bolt", ["M9 1.5 3.5 9H8l-1 5.5L12.5 7H8z"]),
  Hook: makeIcon("hook", ["M8 2.5v6a2.5 2.5 0 0 0 5 0", "M3 8.5a5 5 0 0 0 10 0", "M6 2.5h4"]),
  Help: makeIcon("help", ["M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12Z", "M6.2 6.2a1.9 1.9 0 0 1 3.7.5c0 1.2-1.9 1.5-1.9 2.6", "M8 11.5h.01"]),
  // The issue types, one glyph each, so a list reads by shape before it reads by word.
  Bug: makeIcon("bug", ["M8 5a3 3 0 0 1 3 3v2a3 3 0 0 1-6 0V8a3 3 0 0 1 3-3Z", "M6 5.5 5 3.5", "m10 5.5 1-2", "M5 9H3", "M13 9h-2", "M5.5 12 4 13.5", "m10.5 12 1.5 1.5"]),
  Story: makeIcon("story", ["M5 2.5h6v11l-3-2.2-3 2.2Z"]),
  Epic: makeIcon("epic", ["M9 2 4 9h4l-1 5 5-7H8Z"]),
  Initiative: makeIcon("initiative", ["M4 14V2.5", "M4 3h8.5l-2 3 2 3H4"]),
  Task: makeIcon("task", ["M3 3h10v10H3Z", "m5.5 8 2 2 3.5-4"]),
  Subtask: makeIcon("subtask", ["M4 2.5v6a2 2 0 0 0 2 2h7", "m10.5 8 2.5 2.5-2.5 2.5"]),
} as const;

export type IconName = keyof typeof Icon;
