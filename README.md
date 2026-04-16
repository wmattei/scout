# better-aws-cli

Interactive terminal TUI for navigating and managing AWS infrastructure. Fuzzy-searchable cache over S3 buckets, ECS services/task definitions, Lambda functions, and SSM parameters — with live prefix search into S3 bucket contents, real-time log tailing, and interactive actions (deploy, invoke, update).

## Features

- **Fuzzy search** across all your AWS resources in a single view
- **Service scopes** — type `s3:`, `ecs:`, `lambda:`, `ssm:` to narrow by service
- **S3 drill-in** — Tab into a bucket to browse folders and objects interactively
- **Live log tailing** — CloudWatch Logs streamed in real time with substring filter
- **Lambda invoke** — open `$EDITOR`, write a JSON payload, invoke, see the result
- **SSM management** — copy values to clipboard or update them in-place via `$EDITOR`
- **ECS Force Deploy** — trigger a new deployment with a y/n confirmation gate
- **Profile/region switcher** — hot-swap AWS context with `Ctrl+P` without restarting
- **Persistent cache** — SQLite per `(profile, region)`, populated by `preload` or lazy on first scope entry
- **Debug log** — `BETTER_AWS_DEBUG=1` writes structured JSON to `~/.cache/better-aws/debug.log`

## Install

```bash
go install github.com/wagnermattei/better-aws-cli/cmd/better-aws@latest
```

Or build from source:

```bash
git clone https://github.com/wagnermattei/better-aws-cli
cd better-aws-cli
go build -o bin/better-aws ./cmd/better-aws
```

## Quick start

```bash
# 1. Populate the cache (run once per profile/region)
better-aws preload all

# 2. Launch the TUI
better-aws

# 3. Narrow by service scope
#    Type in the search bar:  ecs:my-service
#                              s3:my-bucket
#                              lambda:my-fn
#                              ssm:/my/param
```

## Supported services

| Service | Scope prefixes | Actions |
|---|---|---|
| S3 Buckets | `s3:`, `buckets:` | Open in Browser, Copy URI, Copy ARN |
| S3 Objects | (drill into bucket) | Open, Copy URI, Copy ARN, Download, Preview |
| ECS Services | `ecs:`, `svc:`, `services:` | Open, Force Deploy, Tail Logs |
| ECS Task Defs | `td:`, `task:`, `taskdef:` | Open, Copy ARN, Tail Logs |
| Lambda Functions | `lambda:`, `fn:`, `functions:` | Open, Copy ARN, Tail Logs, Run |
| SSM Parameters | `ssm:`, `param:`, `params:`, `parameter:` | Open, Copy ARN, Copy Value, Update Value |

## Key bindings

| Key | Action |
|---|---|
| `↑` / `↓` | Navigate results |
| `Tab` | Autocomplete / drill into bucket |
| `Enter` | Open details + actions |
| `Esc` | Go back |
| `Ctrl+P` | Switch AWS profile/region |
| `Ctrl+C` | Quit |
| `/` | Filter (in tail logs view) |
| `Opt+Bksp` | Delete last path segment (S3 breadcrumbs) |

## Environment variables

| Variable | Purpose |
|---|---|
| `AWS_PROFILE` | AWS credentials profile to use |
| `AWS_REGION` | AWS region override |
| `BETTER_AWS_DEBUG=1` | Enable debug log at `~/.cache/better-aws/debug.log` |
| `EDITOR` | Editor for Lambda Run payloads and SSM Update Value |

## Cache

SQLite databases at `~/.cache/better-aws/` (or `$XDG_CACHE_HOME/better-aws/`), one file per `(profile, region)` pair. No automatic expiration — use `better-aws cache clear` to wipe everything.

```bash
better-aws preload all            # preload all services
better-aws preload s3             # preload only S3 buckets
better-aws preload --limit 50 ecs # preload first 50 ECS services
better-aws cache clear            # wipe the cache
```

## Contributing

See `CLAUDE.md` at the project root for the full architecture map, dependency graph, import rules, and guide for adding a new AWS service. The codebase is structured around a `services.Provider` interface — adding a new service is a self-contained change in a new package under `internal/awsctx/`.

## License

MIT — see LICENSE file.
