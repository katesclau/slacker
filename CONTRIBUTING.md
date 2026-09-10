# Contributing

## Running slacker locally

See [README.md](README.md#how-to-run-locally).

## What gates your pull request

`.github/workflows/ci.yml` runs on every pull request:

```
go build ./...
go test ./...
```

The Go version comes from `go.mod`. Tests need no Postgres, Redis, MinIO, Slack token or
API key, so a green check means the same thing on any machine.

Before pushing, run `make check`. It covers the test run and adds formatting checks and
`go vet`, but not `go build`. It needs [ripgrep](https://github.com/BurntSushi/ripgrep).

## Dependency updates

Dependabot opens at most two grouped pull requests a week — one for Go modules, one for
action SHAs — and both are gated by the check above. The rationale is commented in
`.github/dependabot.yml`.

Auto-merge is off. To turn it on, uncomment the `dependabot-automerge` job at the bottom
of `.github/workflows/ci.yml`.

One-time repository setup:

- **On a fork:** Settings → Code security → Dependabot version updates → **Enable**.
  GitHub does not auto-enable version updates on forks that inherit a `dependabot.yml`.
- Only if you swap `gh pr merge --squash` for `--auto`: Settings → General → Pull
  Requests → **Allow auto-merge**.
