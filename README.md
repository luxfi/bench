# luxbench — one bench harness, one way

`luxbench` is the single driver for every benchmark in the luxfi stack.

Before this, /tmp had 5+ ad-hoc bench dirs each with its own orchestration,
output format, and metric names. This is the decomplected replacement.

## Usage

```bash
# Build:
GOWORK=off go build -o /tmp/luxbench ./cmd/luxbench

# List modes:
/tmp/luxbench -list

# Run a mode:
/tmp/luxbench --mode=zap-vs-codec --benchtime=2s --count=3 --output=/tmp/result.json
```

Common flags (every mode sees these via `bench.Config`):

| Flag           | Default | Purpose                                       |
|----------------|---------|-----------------------------------------------|
| `--mode`       | (req)   | Mode name; see `-list`.                       |
| `--benchtime`  | `1s`    | go test -benchtime value (or mode equivalent) |
| `--count`      | `1`     | Runs per subtest (for median / quantile)      |
| `--output`     | (req)   | Output JSON path (always written)             |
| `--verbose`    | `false` | Stream underlying tool output                 |

## Result file

Every mode writes the same envelope:

```jsonc
{
  "version": 1,
  "mode": "zap-vs-codec",
  "records": [
    {
      "mode": "zap-vs-codec",
      "subtest": "BenchmarkRejectBadMagic_ZAP",
      "architecture": "spark",
      "timestamp": "2026-06-04T04:37:53Z",
      "go_version": "go1.26.3",
      "iterations": 1000000000,
      "median": 2,
      "p50": 2, "p95": 2, "p99": 2,
      "bytes_per_op": 0,
      "allocs_per_op": 0,
      "ops_per_sec": 5e8,
      "extra": { "MB/s": 4087460.62 }
    }
  ]
}
```

Schema: `result/schema.go` (`ResultRecord`).

## Modes

| Mode             | Status     | Source                       | What it measures                                          |
|------------------|------------|------------------------------|-----------------------------------------------------------|
| `zap-vs-codec`   | LIVE       | `modes/zap_vs_codec/`        | 8 head-to-head claims: linearcodec vs zap zero-copy        |
| `gpu`            | scaffolded | `modes/gpu/`                 | luxfi/accel + lux-private/gpu-kernels (TODO: port)         |
| `multi-validator`| scaffolded | `modes/multi_validator/`     | 10v / 100v / 500v E2E (TODO: port)                         |
| `parity`         | scaffolded | `modes/parity/`              | crypto-primitive parity audits (corona/pulsar) (TODO)      |
| `matrix`         | scaffolded | `modes/matrix/`              | 7-profile sig-pack matrix (TODO)                           |

The canary `zap-vs-codec` is fully migrated from `/tmp/zap-vs-codec/`.
Other source dirs in `/tmp/` are reproducible outputs and stay there
until their migration sessions land.

## Adding a new mode

1. Create `modes/<name>/` (snake_case dir, snake_case Go package).
2. Implement `bench.Mode` (see `mode.go`):
   - `Name()` — kebab-case (`--mode=<name>`)
   - `Description()` — one line for `-help`
   - `RegisterFlags(*flag.FlagSet)` — mode-specific flags
   - `Run(ctx, bench.Config) ([]result.ResultRecord, error)`
3. Register in `cmd/luxbench/main.go` (alphabetical).
4. Document mode-specific flags in this README's table.

Two integration approaches for wrapping benchmarks:

- **Wrap `go test -bench`** (recommended when the bench is already a Go
  test, like `zap_vs_codec`). The mode shells out to `go test`, parses
  the standard bench output, collapses repeated runs into one
  `ResultRecord` per subtest with median / p50 / p95 / p99. The parser
  is reusable — copy `modes/zap_vs_codec/mode.go`'s `parseBenchOutput`.
- **Native sampling** (for benches that aren't `*_test.go`, e.g. GPU
  kernels driven by a custom Go harness). The mode times its own
  iterations and constructs `ResultRecord` values directly.

## Rule

Every new benchmark MUST go through this harness. No new ad-hoc
`/tmp/<thing>-bench/` directories. If the harness is missing something
your bench needs, extend the harness — don't bypass it.

The /tmp/ output dirs are still fine — they're where you store result
JSON. They are no longer where you write code.

## Analysis (TODO)

Future: `cmd/luxbench-analyze/` to consume `result.File` JSONs and emit
comparison tables / regression flags. Stub goes in `cmd/` when the first
analysis task arrives.

## Migration plan (other 4 modes)

Source bench → target mode → effort estimate (real measure: hours of
porting + verification, not lines of code).

| Source                          | Target mode        | Effort | Notes                                                                                    |
|---------------------------------|--------------------|--------|------------------------------------------------------------------------------------------|
| `/tmp/spark-gpu-bench/`         | `modes/gpu/`       | M      | CGo bench with CUDA kernels. Already has its own `go.mod`. Port runner + parse `MEDIANS.txt`. |
| `/tmp/multi-validator/`         | `modes/multi_validator/` | L | Network harness — already emits JSON close to ResultRecord shape. Map field names + collapse the 3 profiles into Extra.|
| `~/work/lux/threshold/protocols/parity/` | `modes/parity/` | S | Single Go test file; thin wrapper around `go test -run TestParity`. |
| `/tmp/zap-vs-codec/` (matrix part) | `modes/matrix/`  | M      | The 7-profile sig-pack matrix is partially in zap-vs-codec but conceptually a different mode; split it cleanly.|
| (no source yet)                 | `modes/gpu_vtbl/`  | S      | GPU dispatch wiring bench — light port once luxfi/accel vtbl lands.                       |

Each mode lands as one PR. The decomplection is done in steps; the
harness contract is now frozen.
