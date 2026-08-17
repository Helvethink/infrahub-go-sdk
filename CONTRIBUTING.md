# Contributing to the Infrahub Go SDK

Thank you for helping improve the Infrahub Go SDK. Contributions of all sizes
are welcome, including bug reports, documentation fixes, tests, and new SDK
capabilities.

This project aims to provide a small, idiomatic, dependable Go API over
Infrahub. Changes should favor predictable behavior, compatibility,
observability, and ease of use over mechanically exposing the GraphQL API.

## Before you start

For a small bug fix or documentation improvement, feel free to open a pull
request directly. For a substantial feature, public API change, new dependency,
or breaking change, open an issue first so the intended behavior and design can
be agreed on before implementation.

When choosing work:

- Check existing issues and pull requests to avoid duplicate effort.
- Keep each contribution focused on one problem.
- Confirm version-sensitive Infrahub behavior against current official
  documentation or a known server schema.
- Never include credentials, customer schemas, production endpoints, or
  unsanitized production responses in an issue, test, or commit.

## Development setup

You will need:

- Git;
- the Go version declared in [`go.mod`](go.mod);
- `make`;
- `golangci-lint` for the mandatory lint check. CI currently uses the version
  pinned in [`.github/workflows/CI.yml`](.github/workflows/CI.yml).

Fork the repository, clone your fork, and download the dependencies:

```sh
git clone https://github.com/<your-account>/infrahub-go-sdk.git
cd infrahub-go-sdk
go mod download
```

Run the baseline checks before making changes:

```sh
make check
make race
```

Build the command-line client with:

```sh
make build
```

## Repository structure

The root `infrahub` package is the primary client facade. It constructs the
protocol client and exposes domain services.

- `pkg/api` contains HTTP and GraphQL protocol behavior.
- `pkg/<domain>` contains public domain services such as branches, nodes,
  schemas, tasks, repositories, and traversal.
- `internal/cli` contains the reusable implementation of `infrahubctl`.
- `cmd/infrahubctl` is the thin executable adapter.
- `internal/...` contains implementation details that are not public API.
- `docs` contains user-facing guides and compatibility notes.

Domain packages may depend on `pkg/api`, but `pkg/api` must not import domain
packages. Domain packages must not import one another. Coordinate cross-domain
workflows in the root package or a dedicated orchestration layer.

## Making a change

Create a topic branch from the latest default branch and keep the patch focused.
Before editing code, read the nearby implementation and tests.

When changing the SDK:

- Put `context.Context` first on operations that can block and propagate
  cancellation to HTTP requests.
- Keep exported APIs minimal and additive whenever possible.
- Add Go documentation for every exported identifier.
- Wrap errors with `%w` when callers may need the underlying cause.
- Do not mutate caller-owned HTTP clients, transports, or shared request state.
- Preserve base URL paths and structurally escape branch names and identifiers.
- Treat attributes and relationships as distinct concepts.
- Preserve absent and explicit `null` values when mutation semantics differ.
- Avoid new dependencies unless they materially improve correctness or
  maintainability. Explain the tradeoff in the pull request.

Public names, method signatures, option behavior, serialized request shapes,
and error matching are compatibility commitments. If a breaking change is
unavoidable, discuss it in an issue and include migration guidance.

## Commit and pull request titles

Use a short, descriptive commit subject. Keep the pull request title consistent
with the change because squash merges commonly use it as the resulting commit
subject.

Use these prefixes for maintenance-only changes that should be identifiable by
the release-note automation:

- `ci:` for pipelines, workflows, and build infrastructure;
- `chore:` for GitHub templates, Dependabot, and repository metadata;
- `docs:` for contribution guidelines and community process
  documentation.

For example:

```text
ci: update the release pipeline
chore(github): add issue templates
docs(contributing): document release-note conventions
```

These prefixes allow matching commits to be omitted from generated release
notes. They do not remove commits from Git history or release tags. Do not use a
maintenance prefix to hide a user-visible behavior change, migration note, or
security fix from a release.

## GraphQL and Infrahub schemas

Infrahub is schema-driven and branch-aware. Do not assume a custom kind or field
exists on every server or branch.

- Use the target branch schema for branch-specific dynamic operations.
- Put caller-provided values in GraphQL variables; never interpolate them into
  a query document.
- Validate any GraphQL identifier that must be inserted into a document.
- Decode GraphQL `data` and `errors` independently because both may be present.
- Distinguish transport, HTTP, GraphQL, decoding, and domain-operation errors.
- Do not retry mutations by default. Any query retry must be bounded,
  cancellation-aware, and safe.

Record the Infrahub server or Python SDK version used to confirm a
version-sensitive contract.

## Tests

Every behavior change needs tests for its observable contract and relevant
failure modes. Prefer deterministic table-driven tests and `httptest.Server`
over mocks of HTTP internals.

Depending on the change, cover:

- successful request and response handling;
- GraphQL errors and partial data;
- non-2xx HTTP responses;
- malformed and oversized responses;
- cancellation and deadlines;
- URL and branch escaping;
- request headers, variables, and secret redaction;
- pagination and concurrent use.

Keep unit tests independent of a live Infrahub instance. Integration tests must
be explicitly gated and must obtain endpoints and credentials from the
environment.

Run the narrowest relevant tests while iterating:

```sh
go test ./pkg/<domain>
```

Before submitting a pull request, run:

```sh
make check
make race
```

If command code changed, also run:

```sh
make build
```

`make check` verifies formatting, runs `go vet`, runs `golangci-lint`, and runs
the complete test suite. Do not disable a linter or exclude a test merely to
make a check pass.

Run `go mod tidy` only when imports or module requirements change, and review
the resulting `go.mod` and `go.sum` diff.

## Documentation

Update documentation whenever behavior visible to users changes:

- Keep README examples centered on the root client facade.
- Add or update a guide in `docs` for non-obvious behavior.
- Update [`docs/compatibility.md`](docs/compatibility.md) when adding or
  intentionally omitting a Python SDK capability.
- Keep examples compilable and free of real credentials or customer data.

## Pull request checklist

Before requesting review, confirm that:

- the pull request explains the problem and the chosen behavior;
- the change is focused and preserves unrelated work;
- new or changed behavior has meaningful tests;
- exported APIs and compatibility implications are called out;
- documentation and examples are current;
- `make check` and `make race` pass;
- `make build` passes when command code changed;
- no credentials or sensitive response data appear in code, logs, fixtures, or
  the pull request description.

If a check could not be run, state that clearly in the pull request along with
the reason.

## Reporting security issues

Do not open a public issue for a suspected vulnerability or include exploit
details, tokens, or sensitive server responses in public. Contact the project
maintainers privately and allow time for investigation before public
disclosure.

Contributions are provided under the repository's [MIT License](LICENSE.md).
