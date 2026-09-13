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
`WORKERS=<n>` sets how many browsers run it (four unless said otherwise).

The API is described by `api/openapi.json`, derived from the handler tables and
types. After changing a route, a request type or a response type, run
`make openapi` and commit the regenerated document and `web/src/api/schema.d.ts`;
a unit test refuses a stale copy, and the integration suite validates every
response against it and refuses a run that left an operation unexercised.

Nothing is mocked below the layer under test. A guard is not proven by the
service refusing: try the same thing straight through SQL and watch the database
refuse it too.

## Style

- Doc comments say why, in at most two lines. The code already says what.
- No en dashes and no typographic quotes, in code or in prose.
- Named constants over magic numbers. Frontend ones live in `web/src/config.ts`.
- Errors the user reads are sentences, and name what to do about it.
