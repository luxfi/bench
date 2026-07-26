# bench — luxbench unified harness

Single bench driver for the luxfi stack. Replaces the ad-hoc
`/tmp/<name>-bench/` directories.

## Layout

```
bench/
├── cmd/luxbench/        # CLI entry: flag parsing + mode dispatch
├── mode.go              # Mode interface (Name/Description/RegisterFlags/Run)
├── modes/
│   ├── zap_vs_codec/    # CANARY: 8 head-to-head linearcodec vs zap claims
│   ├── gpu/             # scaffold (TODO: port /tmp/spark-gpu-bench/)
│   ├── multi_validator/ # scaffold (TODO: port /tmp/multi-validator/)
│   ├── parity/          # scaffold (TODO: port lux/threshold/protocols/parity/)
│   └── matrix/          # scaffold (TODO: 7-profile sig-pack matrix)
├── result/schema.go     # ResultRecord + File envelope + JSON IO + quantiles
└── README.md            # user-facing docs
```

## Invariants

- One harness. No new `/tmp/<thing>-bench/` directories — every new
  bench is a mode under `modes/`.
- One result schema (`result.ResultRecord`). Mode-specific metrics
  go in `Extra` (snake_case keys).
- Modes implement `bench.Mode`; CLI dispatches by `--mode=<name>`.
- The CLI owns JSON serialization. Modes return `[]ResultRecord`,
  never write files directly.

## Building

```bash
cd ~/work/lux/bench && GOWORK=off go build ./...
GOWORK=off go test ./...
```

`GOWORK=off` because the harness has its own `go.mod` with local
replace directives for `luxfi/codec` and `luxfi/zap`. Don't add it
to the workspace until the upstream tags catch up.

## Canary verification

`zap-vs-codec` mode produces the same numbers as the original
`/tmp/zap-vs-codec/` would have, within run-to-run noise. Deterministic
metrics (B/op, allocs/op) are identical:

```bash
GOWORK=off /tmp/luxbench --mode=zap-vs-codec \
  --bench='^BenchmarkRejectBadMagic_(Codec|ZAP)$' \
  --benchtime=2s --count=3 --output=/tmp/x.json
```

vs. running `go test -bench=...` directly in `modes/zap_vs_codec/` —
the bench source is the same files, the harness only wraps execution
and parses output.
