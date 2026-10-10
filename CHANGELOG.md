# Changelog

What changed in each release, newest first. Versions follow
[semantic versioning](https://semver.org); `make release VERSION=x.y.z` moves
everything under Unreleased into a section of its own, so a change is written
here in the branch that makes it.

## Unreleased

- Fixed: deleting a team that still has sprints or request types routed to
  it is refused with a sentence saying what to do, instead of handing its
  sprints to the project or failing; a team that has run a sprint is kept,
  and the database refuses the delete too (#55).
- Fixed: one issue dated far in the future no longer makes every plan read
  that far: the load is read over at most ten years, and issue dates
  outside 1900 to 2199 are refused with a sentence, also by the database (#40).
- Share of the week: a project administrator sets how much of each person's
  week the project has, and the Resources page counts their hours at it, per
  person and inside their team; a person given more than a whole week across
  projects is warned about, never refused (#43).
- Fixed: Themes has an icon of its own, a palette, instead of the sun the
  light mode wears, in the sidebar, the settings page and the command palette
  (#122).
- Fixed: somebody who has left the organization no longer shows on the
  Resources page with a full week free; work still assigned to them is counted
  as nobody's, and the project calendar no longer counts them among its people
  (#42).
- Fixed: importing an .ics file again no longer turns a calendar's half-day
  holidays into whole days off; a day the calendar has takes only the file's
  name (#45).
- Fixed: an .ics event that gives its length with DURATION (such as P3D or
  P1W) is imported for every day it covers, and one whose end is its start day
  is that one day instead of refusing the file (#44).
- Fixed: the Resources page counts work once per row, at the highest open level
  that carries hours, so a story and its subtasks are no longer booked twice
  (#41).
- Resource planning: a Resources page sets each week's scheduled hours against
  the hours left after holidays and absences, per team, or per person in a
  kanban project, hatches the days off, lists a week's issues on a click and the
  unscheduled and unestimated work below; projects gain a planning method and a
  grouping, and a scrum project plans by team (#31).
- Holidays and absences reach planning: the project calendar shades the
  default calendar's holidays, lists the other calendars' and who is away, and
  hides them all at a switch; the timeline shades the holidays; a team's load
  falls on its working days and its weekly capacity shrinks with its members'
  days off; sprint planning shows the team's available hours as a hint (#30).
- Absences: a person records the days they are away on their profile, and an
  administrator or the scrum master of their team may record them too; every
  colleague sees the days, never a reason, and customers see nothing (#29).
- Holiday calendars and working weeks: an organization keeps named calendars of
  days off, typed in or imported from an .ics file, and an administrator gives
  each person a calendar and their hours per weekday (#28).
- The application is versioned: `VERSION` names the release, `make release`
  cuts one, and a build past a tag says how far past it is (#27).
- Everything before this point: projects and issues, workflows, roles,
  planning, git, the service desk, NQL, dashboards, notifications, webhooks,
  automation, themes and the API, as the README describes them.
