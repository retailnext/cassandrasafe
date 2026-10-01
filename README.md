# cassandrasafe

cassandrasafe waits until a Cassandra node and its peers are ready for the next
step of a rolling operation. Run it next to a node after you restart that node.
It exits with code 0 when every peer in the same datacenter answers CQL queries
a required number of times in a row. It does not exit while a peer is down.

The project publishes a container image for `linux/amd64` and `linux/arm64` at
`ghcr.io/retailnext/cassandrasafe`.

## How it decides that the ring is ready

1. It connects to the node at `--host` and reads `system.local` and
   `system.peers`. It repeats this until the node answers.
2. It keeps the peers that are in the same datacenter as the node. It drops the
   peers in other datacenters and logs each one.
3. It queries every kept peer at the same time. A peer that answers reports the
   tokens that it owns. The check passes when the reported tokens cover every
   token that `system.peers` lists. A peer that owns no live tokens, such as a
   stale row for a replaced node, cannot block the check.
4. After a passed check, it waits `--pass-interval` and checks again. After
   `--required-passes` passed checks in a row, it exits with code 0. A failed
   check resets the count, and the next check starts after `--retry-interval`.

## Run it as a sidecar

Run the image in the network namespace of the Cassandra container. Then
`127.0.0.1` is the node, and the program can reach the peers at the addresses
that the node knows.

<!-- renovate: datasource=github-tags depName=retailnext/cassandrasafe -->
```bash
docker run --rm --network container:cassandra ghcr.io/retailnext/cassandrasafe:v0.1.0
```

Replace `cassandra` with the name of the Cassandra container. The example
shows the latest release. Renovate updates it after each release. CI publishes
only released versions. There is no `latest` tag. Each
[release](https://github.com/retailnext/cassandrasafe/releases) lists the
exact command for its version.

To check a node from another host, give the address with `--host`:

<!-- renovate: datasource=github-tags depName=retailnext/cassandrasafe -->
```bash
docker run --rm ghcr.io/retailnext/cassandrasafe:v0.1.0 --host 192.0.2.10
```

## Flags

| Flag                | Default     | Meaning                                                                  |
| ------------------- | ----------- | ------------------------------------------------------------------------ |
| `--host`            | `127.0.0.1` | Host, or `host:port`, of the node to query first.                        |
| `--required-passes` | `6`         | Number of passed checks in a row that lets the program exit.             |
| `--pass-interval`   | `5s`        | Pause after a passed check.                                              |
| `--retry-interval`  | `1s`        | Pause before the program queries a host again after a failure.           |
| `--status-interval` | `1s`        | Pause between log lines that list the hosts that did not answer yet.     |
| `--log-format`      | `auto`      | `auto`, `text`, or `json`. `auto` is `text` on a terminal, else `json`.  |
| `--log-level`       | `info`      | `debug`, `info`, `warn`, or `error`.                                     |
| `--version`         |             | Print the version and exit.                                              |

## Exit codes and signals

| Code | Meaning                                                              |
| ---- | -------------------------------------------------------------------- |
| `0`  | The required number of checks passed in a row.                       |
| `1`  | The program stopped before that. The last log line gives the reason. |
| `2`  | A flag was not valid.                                                |

`SIGINT` and `SIGTERM` stop the program. It then exits with code 1.

## Logs

The program writes logs to stderr. On a terminal it writes text. Otherwise it
writes one JSON object per line. Use `--log-level debug` to see each query
attempt. At the default level, a line like this one appears every
`--status-interval` while a peer does not answer:

```text
level=INFO msg="waiting for hosts" outstanding_hosts=[192.0.2.20] outstanding_tokens=16
```

## Supported Cassandra versions

The continuous integration runs the smoke test in `e2e/` against a two-node
cluster of each supported major version: Cassandra 3.11, 4.1, and 5.0. The
driver negotiates native protocol version 3, 4, or 5.

## Limitations

- The program connects without credentials. Clusters that require
  authentication are not supported.
- The program queries each peer at its `peer` address from `system.peers`, on
  port 9042. Clusters where the client address of a node differs from its
  broadcast address are not supported. A port given with `--host` applies only
  to the first node.

## Get an image from a CI run

Every CI run uploads the image as a run artifact named `image`. The artifact
is an OCI image layout with both platforms. Use it to test a change before a
release:

```bash
gh run download <run-id> --repo retailnext/cassandrasafe --name image --dir image
regctl image export --platform local --name cassandrasafe:ci ocidir://image:ci | docker load
docker run --rm cassandrasafe:ci --version
```

[regctl](https://github.com/regclient/regclient) reads the layout and exports
the image for your platform in the format that `docker load` accepts.

## Development

Tools:

- Go, at the version in `go.mod`
- [prek](https://prek.j178.dev/), which runs the hooks in
  `.pre-commit-config.yaml`
- [ko](https://ko.build/) for the image, and
  [regctl](https://github.com/regclient/regclient) to load an image from a CI
  artifact
- Docker with Compose, and GNU `timeout`, for the integration and smoke tests

Run the checks:

```bash
prek install
prek run --all-files
```

Run the unit tests, the integration test, and the coverage check. The
integration test needs a Cassandra node and runs only when you set
`CASSANDRASAFE_TEST_HOST`:

```bash
docker compose -f e2e/compose.cassandra-5.yaml up -d node1
until docker exec cassandrasafe-e2e-node1 cqlsh -e 'describe cluster' >/dev/null 2>&1; do sleep 5; done
CASSANDRASAFE_TEST_HOST=127.0.0.1 go test -race -coverprofile=coverage.out -coverpkg=./... ./...
go tool cover -func=coverage.out | tail -1
docker compose -f e2e/compose.cassandra-5.yaml down -v
```

Build the image for your platform and load it into Docker in one step. ko
prints the image reference:

```bash
IMAGE=$(ko build --local --platform="linux/$(go env GOARCH)" .)
docker run --rm "$IMAGE" --version
```

Run the smoke test against each supported major version. Each run takes a few
minutes:

```bash
for major in 3 4 5; do
  e2e/smoke.sh "$IMAGE" "e2e/compose.cassandra-$major.yaml"
done
```

CI builds the image for both platforms into an OCI layout instead, because the
layout is the artifact that the other jobs and the publish step use. See the
`Go` job in `.github/workflows/ci.yaml` for those commands.

See [AGENTS.md](AGENTS.md) for the code structure and the rules that the
linters enforce.

## Continuous integration

The `CI` workflow runs on pull requests, on pushes to `main`, and on `v*` tags:

- `Lint` runs the prek hooks, except golangci-lint.
- `Go` runs golangci-lint, the tests with the integration test, the coverage
  check, and the image build. It uploads the image as the `image` artifact.
- `Smoke test (Cassandra N)` runs the smoke test once per supported Cassandra
  major version, in parallel, with the image from the `Go` job.
- `Smoke tests` passes when every smoke test job passed. Branch rules reference
  this job, so the list of Cassandra versions can change without a change to
  the rules.
- `Publish the image` runs only for a `v*` tag. It checks that the tag points
  at a commit on `main` and that the tag is not in the registry yet. Then it
  copies the image from the artifact to
  `ghcr.io/retailnext/cassandrasafe:<tag>` and creates a GitHub Release with
  generated notes and the `docker run` command for that version.

## Releases

Releases are git tags of the form `vX.Y.Z`. Only the `Release` workflow can
create them:

1. Open the `Release` workflow in the Actions tab and run it on `main`. Select
   `patch`, `minor`, or `major`. The first release is always `v0.1.0`.
2. The workflow checks that CI passed for the commit at the head of `main`,
   computes the next version, and pushes an annotated tag with a GitHub App
   token.
3. The tag push starts the `CI` workflow, which tests the commit again,
   publishes the image, and creates the GitHub Release.
4. Renovate opens a pull request that updates the version in the README
   examples. The Renovate approver bot approves it, and Renovate merges it
   when the checks pass.

Nobody changes or deletes a published tag.

## License

Apache License 2.0. See [LICENSE](LICENSE).
