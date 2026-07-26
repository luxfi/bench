# modes/matrix — TODO (Phase 5)

Port the 7-profile sig-pack matrix bench here. Source is the
matrix-shaped portion of the lux/threshold profile sweep — it walks
{strict-pq, hybrid, classical} × {1, 4, 16, 64 validators} and
reports per-cell throughput + latency.

- Mode-specific flags: `--profiles=all|strict-pq|hybrid|classical`,
  `--validators=1,4,16,64`.
- Native sampling: matrix harness times each cell.
- Extras to surface: `profile`, `validator_count`, `signs_per_sec`,
  `verify_per_sec`, `pack_bytes`.

Until ported, run the legacy bench out of the threshold profile sweep
scripts.
