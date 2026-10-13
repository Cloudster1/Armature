# Working on Armature

## Nothing is installed on this machine

Every toolchain runs in a container, driven by the `Makefile`. Never install Go,
Node, Chromium or a Postgres client on the host. If a tool is missing, add a
target that runs it in a container.

Go is pinned to 1.26 on purpose. Under 1.27 on this host every test binary is
killed about a second into its run. See "Build notes" in the README.

## Every feature gets its own branch

`main` is always the working product. Nothing is committed straight to it.

```sh
git switch -c feature/<what-it-adds>   # or fix/<what-it-repairs>
make test-all                          # all four layers, on the branch
git switch main
git merge --no-ff feature/<what-it-adds>
```

The merge commit is kept so the branch stays visible as one unit of work.
Commits describe a change to the product, not to a file.

## Testing

`make test`, `make test-integration`, `make npm ARGS="run test"` and
`make test-e2e`. A feature is finished when all four pass, not when the code is
written. `ONLY=<substring>` filters the browser suite while iterating and
`WORKERS=<n>` sets how many browsers run it (four unless said otherwise), and
`SHUFFLE=1` runs it in a random order. It
runs against the production build, so a CSP or nginx problem fails it;
`E2E_WEB=dev` points it at the Vite dev server instead.

The API is described by `api/openapi.json`, derived from the handler tables and
types. After changing a route, a request type or a response type, run
`make openapi` and commit the regenerated document and `web/src/api/schema.d.ts`;
a unit test refuses a stale copy, and the integration suite validates every
response against it and refuses a run that left an operation unexercised.

An integration test calls `t.Parallel()` first thing, or is listed in
`backend/test/parallel_test.go` with what it shares that a parallel test would
disturb; `make test` refuses one that is neither.

A browser scenario shares one installation with every other scenario of the run.
It signs in as a new person (`signUp`), keeps to its own projects, which
`createProject` gives unique keys, and gives anything everybody shares (labels,
statuses, workflows, schemes, groups, roles, fields, templates, webhooks, shared
filters) a name from `uniqueName`. A scenario that changes something every
scenario lives with, such as a default or the second-factor rule, is registered
with `serialScenario` and puts it back in a `finally`; the runner fails one that
makes such a request from the parallel pool, from the list in `e2e/runner.mjs`.

Nothing is mocked below the layer under test. A guard is not proven by the
service refusing: try the same thing straight through SQL and watch the database
refuse it too.

## Style

- Doc comments say why, in at most two lines. The code already says what.
- No en dashes and no typographic quotes, in code or in prose.
- Named constants over magic numbers. Frontend ones live in `web/src/config.ts`.
- Errors the user reads are sentences, and name what to do about it.
- Copy says "everyone" or "all projects", never "organization"; `web/src/lib/words-guard.test.ts` refuses it outside the Installation page.
