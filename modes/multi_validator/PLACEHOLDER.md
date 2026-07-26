# modes/multi_validator — TODO (Phase 3)

Port `/tmp/multi-validator/` to this directory:

- `harness.go` + the build artifacts (`bench-darwin-arm64`,
  `thresholdd-darwin-arm64`) move here. The harness orchestrates a
  10v / 100v / 500v validator network across 3 consensus profiles
  (aurora / hybrid / polaris) and emits one JSON per tier×profile.
- Mode-specific flags: `--scale=10|100|500`, `--profile=aurora|hybrid|polaris`.
- Native sampling (NOT `go test -bench` wrapping) — the harness times
  its own rounds and constructs ResultRecord directly.
- Extras to surface (current keys from `/tmp/multi-validator/result-*.json`):
  `blocks_per_sec`, `bls_p50_ms`, `pulsar_p50_ms`, `corona_p50_ms`,
  `magnetar_p50_ms`, `finality_p50_ms`, `cert_tier_final`,
  `host_cpu_max`, `host_mem_max_mb`.

Until ported, run the legacy bench out of `/tmp/multi-validator/`.
