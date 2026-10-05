# Changelog

What changed in each release, newest first. Versions follow
[semantic versioning](https://semver.org); `make release VERSION=x.y.z` moves
everything under Unreleased into a section of its own, so a change is written
here in the branch that makes it.

## Unreleased

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
