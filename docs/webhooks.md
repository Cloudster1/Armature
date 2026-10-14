# Webhooks

## Topics

### issue.transitioned

An issue moved from one status to another, through a workflow transition or
because a type change put it in the new workflow's status.

| Field | What it holds |
| --- | --- |
| `issueId` | the issue's id |
| `key` | the issue's key, such as `NOJ-7` |
| `transition` | the name of the transition taken, or `Type change` when a type change moved the issue |
| `fromStatus` | the name of the status the issue left |
| `toStatus` | the name of the status the issue is in now |
| `fromStatusCategory` | the category of the status it left: `todo`, `in_progress` or `done` |
| `toStatusCategory` | the category of the status it is in now: `todo`, `in_progress` or `done` |
| `actorId` | the id of the person who moved it |

The categories let a subscriber keep an issue's category without reading the
issue again.
