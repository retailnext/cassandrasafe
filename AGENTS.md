# AGENTS.md

This file gives the rules for agents and people who change this repository.

## What the program does

cassandrasafe runs next to a Cassandra node. It reads `system.local` and
`system.peers` from that node, keeps the peers in the same datacenter, and
queries each peer until it answers. A check passes when the tokens that the
peers report cover every token in `system.peers`. The program exits with code 0
after the required number of passed checks in a row. It exits with code 1 when
the context ends first, and with code 2 when a flag is not valid.

The behavior contract, in order:

1. Discovery repeats until the first node answers. It fails only when the
   context ends. The error then wraps both `context.Canceled` and the last
   driver error.
2. The check queries every peer at the same time. A peer that answers accounts
   for the tokens that it reports, not the tokens in `system.peers`. The check
   ends when the peers account for every token, when every host answered, or
   when the context ends. A check that ends with tokens outstanding returns
   `*peercheck.UnresponsiveHostsError`. When the context ended, the error also
   wraps `ctx.Err()`.
3. The steady state loop counts passed checks. A failed check resets the count.
   The loop branches on `ctx.Err()`, never on the error type. After a failed
   check it waits `--retry-interval`. After a passed check it waits
   `--pass-interval`.
4. Each driver session uses one connection, `DisableInitialHostLookup`, and
   consistency `LocalOne`. The driver negotiates the protocol version. Keep
   these settings. They let a single node answer without a ring lookup.
5. `main` handles `SIGINT` and `SIGTERM` through `signal.NotifyContext`.

Do not change this contract without a change to the README and to the tests.

## Writing rules

Write all text for this repository in ASD-STE100 Simplified Technical
English. This applies to:

- the README, this file, and all other documentation
- flag help, log messages, and error strings
- Go doc comments and code comments
- commit messages
- pull request titles and descriptions
- review comments, issue text, and release notes

Use American English where the standard is silent.

Use the `asd-ste100` skill for every piece of text in that list. Before you
commit, check each changed text file with the skill's linter,
`scripts/ste-lint.py`, and remove every hard violation. Check a commit message
or a pull request description the same way: write the text to a file, run the
linter on the file, then use the text. If the skill is not installed, install
it from <https://github.com/danyuchn/asd-ste100-skill>:

```bash
npx skills add danyuchn/asd-ste100-skill
```

Or clone that repository into the skills directory of your agent. The
repository does not vendor the skill and does not run its linter in CI. The
standard is a rule for authors, not a build step.

Use strict mode for flag help, error strings, log messages, review comments,
and this file. Use STE-flavored mode for the README, commit messages, and pull
request descriptions.

## Build, test, lint

```bash
prek install                      # once
prek run --all-files              # all hooks, including golangci-lint
go test -race ./...               # unit tests
```

The coverage threshold is 90 % of all statements. CI enforces it. The
threshold assumes that the integration test ran. The integration test needs a
Cassandra node and runs only when you set `CASSANDRASAFE_TEST_HOST`:

```bash
docker compose -f e2e/compose.cassandra-5.yaml up -d node1
until docker exec cassandrasafe-e2e-node1 cqlsh -e 'describe cluster' >/dev/null 2>&1; do sleep 5; done
CASSANDRASAFE_TEST_HOST=127.0.0.1 go test -race -coverprofile=coverage.out -coverpkg=./... ./...
go tool cover -func=coverage.out | tail -1
docker compose -f e2e/compose.cassandra-5.yaml down -v
```

Without `CASSANDRASAFE_TEST_HOST`, the tests cover the driver adapter only on
its error paths and the total is lower. That is not a regression.

Build the image for your platform, load it into Docker, and run the smoke
test. Outside CI, always use `ko build --local`. Do not save and load an image
by hand:

```bash
IMAGE=$(ko build --local --platform="linux/$(go env GOARCH)" .)
for major in 3 4 5; do e2e/smoke.sh "$IMAGE" "e2e/compose.cassandra-$major.yaml"; done
```

Give `--platform` explicitly. Without it, ko builds the two platforms from
`.ko.yaml` and loads the `linux/amd64` image on every host, because ko reads
`GOOS` and `GOARCH` from the environment and defaults to `linux/amd64`.

## Code structure

| Package         | Role           | Concern                                                     |
| --------------- | -------------- | ----------------------------------------------------------- |
| `node`          | data types     | `Local`, `Peer`, `TokensByHost`. No logic.                  |
| `cassandra`     | implementation | Reads system tables through the driver. Only driver import. |
| `peerdiscovery` | consumer       | Finds the same-datacenter peers and their tokens.           |
| `peercheck`     | consumer       | Queries every peer until the peers account for every token. |
| `steadystate`   | orchestrator   | Repeats the check until it passes enough times in a row.    |
| `main`          | root           | Parses flags with Kong, builds the logger, wires the rest.  |

Each consumer defines the interface that it needs. `*cassandra.Client`
satisfies `peerdiscovery.Inspector` and `peercheck.Inspector`.
`*peercheck.Checker` satisfies `steadystate.Checker`. `main.go` defines
`discoverer` and `checker` for its own `run` function. `main.go` is the only
file that imports both a consumer and an implementation.

## Core model

Four ideas are the base of every rule below.

1. **Organize by domain concern, not by mechanism.** A concern is a part of
   what the program does, with a name that a Cassandra operator understands
   without the code. If the best description of a package says how it works,
   such as "runs queries" or "retries", the package has a mechanism name.
   Reorganize it until the description says what problem it solves.
2. **Invert dependencies through consumer-defined interfaces.** The package
   whose logic needs a behavior defines the interface for it. The package that
   provides the behavior satisfies the interface and never imports the
   consumer.
3. **Wire concrete types at the composition root.** `main.go` imports both
   sides, constructs the concrete types, and passes them in. No other package
   knows both sides.
4. **The root resolves inputs, the other packages do not.** `main.go` parses
   flags, reads the environment, and detects the terminal. The other packages
   receive plain values and interfaces.

## Gate 1: package layout

Apply these checks before you add a package, a file, a type, or a function.

1. The package name describes a concern. Names such as `util`, `helpers`,
   `common`, and `client` are not allowed.
2. State the purpose of the package as one noun phrase. If the phrase needs
   "and", "or", or a comma, split the package.
3. Code that its own `_test.go` can exercise without the rest of the package is
   a separate concern. Give it a package.
4. Structs that other packages consume live in `node`. `node` imports only the
   standard library and holds no logic.
5. The consuming end of each dependency defines the interface.
6. `steadystate` sequences steps and holds no infrastructure interface.
7. Split by concern. "It is small" and "only one package uses it" are not
   reasons to merge concerns. Do not split one concern into packages without
   their own purpose.
8. A non-test `.go` file addresses one concern.

## Gate 2: dependency wiring

Apply these checks before you write an import, use a type from another
package, or add a function.

1. The consumer defines the interface. The consumer does not import the
   implementation. The implementation does not import the consumer.
2. `main.go` wires the concrete types.
3. Constructors return concrete structs. Functions accept interfaces.
   `ireturn` enforces the first half.
4. `depguard` enforces the import boundaries: only `cassandra` imports the
   driver, only `main.go` imports Kong, and `node` imports only the standard
   library.
5. Before you write logic that belongs to a different concern than the current
   package, stop. Add it to the package for that concern, or create one. Then
   define an interface in the consumer and wire it in `main.go`.

## Gate 3: functions and methods

1. `ctx context.Context` is the first parameter of every function that does
   network I/O or waits. Never store a context in a struct. `containedctx`,
   `contextcheck`, and `fatcontext` enforce this.
2. Below `main.go`, inject `*slog.Logger` and other standard library
   dependencies. Never call `os.Getenv`, `log.*`, or package-level `slog.*`
   outside `main.go`. `forbidigo` and `sloglint` enforce this.
3. Build loggers only in `main.go`, or in tests on `t.Output()`. `forbidigo`
   bans `slog.New`, `slog.NewTextHandler`, and `slog.NewJSONHandler`
   everywhere else, and bans `slog.Default`, `slog.SetDefault`,
   `slog.DiscardHandler`, `io.Discard`, and the process streams outside
   `main.go`.
4. Log through the injected logger. Use the `...Context` methods when a context
   is in scope. Messages are static, lowercase, and in STE. Keys are
   `snake_case` constants. `sloglint` enforces this.
5. No globals and no `init`. `gochecknoglobals` allows `err`-prefixed error
   variables, `_`, `regexp.MustCompile` results, and `//go:embed` variables.
6. Do not use `context.TODO`. `context.Background` appears only in `main`.
7. Wrap errors with `%w` and compare with `errors.Is` or `errors.As`.
   `wrapcheck` and `errorlint` enforce this.
8. Do not add an `internal` directory.
9. Timers and retries are plain `select` loops in the package that needs them.
   There is no shared helper package.

## Gate 4: tests

1. Unit tests live in the package's `_test.go` files and use fakes for the
   interfaces. Fixture addresses use the RFC 5737 ranges `192.0.2.0/24` and
   `198.51.100.0/24`.
2. The integration test is `TestIntegration_SingleNode` in
   `main_integration_test.go`. It is the only test that runs the driver
   against a server. `forbidigo` has an exclusion for `os.Getenv` in that
   file only.
3. `t.Parallel()` is the first statement of every test and subtest.
   `paralleltest` and `tparallel` enforce this.
4. Use `t.Context()`, never `context.Background()`. `usetesting` enforces
   this.
5. Build test loggers with `slog.New(slog.NewTextHandler(t.Output(), nil))`.
   Never discard logs.
6. Use `t.Cleanup()` for teardown.
7. Run tests with `-race`. Measure coverage with `-coverprofile=coverage.out
   -coverpkg=./...`. Do not commit code that takes the total below 90 %.
8. No goroutine that a function starts may outlive its return. Tests that
   cancel a context must assert that the function returns.

## Gate 5: continuous integration

`ci.yaml` has one job that uses `actions/setup-go`: `go`. Never add
`setup-go` to another job. All jobs share the module cache key, and the
shortest job would fill the cache before the others.

- `Lint` runs prek with `SKIP: golangci-lint-full`. golangci-lint runs in
  `Go` with the version from `GOLANGCI_LINT_VERSION`.
- `Go` runs the tests against a Cassandra node that it starts from
  `e2e/compose.cassandra-5.yaml`, enforces the coverage threshold, builds the
  image for both platforms into the OCI layout `image/`, and uploads the
  layout as the artifact `image`. The layout path must stay a relative
  lowercase path. ko builds an image reference from it, and an image reference
  rejects uppercase letters and a leading slash. ko writes the layout without
  a tag. The `regctl image copy` step adds the tag `ci` that the other jobs
  use.
- `Smoke test (Cassandra N)` is a matrix with one job per compose file in
  `e2e/`. Each job loads the artifact and runs `e2e/smoke.sh`. To add a
  Cassandra major version, add a compose file and add the number to the
  matrix.
- `Smoke tests` is the collector. Branch rules reference the job names
  `Lint`, `Go`, and `Smoke tests`. When you rename one of these jobs, update
  `.github/rulesets/main-branch.json`.
- `Publish the image` runs for `v*` tags only. It never overwrites a tag.
  After the copy to ghcr.io it creates the GitHub Release for the tag.

Give every job and every step a `name`. Pin every action to a full commit SHA
with a version comment. Renovate updates the pins.

## Copyright headers

Every `.go` file starts with the Apache 2.0 header. `goheader` checks the
header, with `{{MOD-YEAR}}` for the year. The year is the year of the last
commit that changed the file. For a new file without history, it is the
current year. A wrong year fails the lint. When you create a file, write the
current year.

Put one empty line between the header and the next line, which is the package
doc comment or the `package` clause. Without the empty line, Go treats the
header as the package doc comment. `gofmt` then reformats the indented URL
line of the header, which fails `goheader`, and `revive` reports a
`package-comments` violation. Only one file in each package carries the
`// Package ...` doc comment, directly above its `package` clause.

## Linter suggestions

When a linter suggests a replacement symbol, check that the symbol exists in
the Go version of `go.mod` with `go doc <symbol>` before you choose a different
fix. The linter runs against the real toolchain.

## Versions

Renovate runs once a month and groups every update into one pull request. It
manages the Go directive, the Go modules, the prek hook revisions, the action
SHAs, the `*_VERSION` variables in the workflows, the Cassandra images in
`e2e/`, the digest of the ko base image in `.ko.yaml`, and the release version
in the README examples. Each compose file in `e2e/` follows one Cassandra major
version. Renovate does not move it to the next major version. The README
version is the one dependency outside the monthly group. Renovate updates it
as soon as a release exists, and the pull request merges itself when the
checks pass. Keep the `<!-- renovate: ... -->` comment on the line before each
example that shows the version.

Before you write a version string by hand, look up the current release. Do not
write a version from memory.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/):
`<type>(<scope>): <description>`. Common types are `feat`, `fix`, `refactor`,
`test`, `docs`, `chore`, and `ci`. Write the description and the body in STE,
as the writing rules above say. One sentence states one fact. Say what changed
and why, not how you found it.

Before a commit:

1. `prek run --all-files`
2. The test and coverage commands above
3. `git status` and `git diff`
4. Stage files by path. Do not use `git add -A`.
