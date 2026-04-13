# better-aws

Interactive terminal CLI for navigating AWS infrastructure. Fuzzy-searchable cache over S3 buckets, ECS services, and task definitions; live prefix search into S3 bucket contents.

**Status:** v0 in development — see `docs/superpowers/specs/2026-04-13-better-aws-cli-v0-design.md` for the spec and `docs/superpowers/plans/` for the phase plans.

## Build

```bash
go build -o bin/better-aws ./cmd/better-aws
./bin/better-aws
```

## Install

```bash
go install ./cmd/better-aws
```
