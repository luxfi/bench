# modes/gpu — TODO (Phase 2)

Port `/tmp/spark-gpu-bench/` to this directory:

- `bls.go`, `cpu.go`, `cuda_host.{c,h}u`, `gpu_cgo.go`, `sha256_kernel.cu`
  move here verbatim (CGo bench glue).
- `bench_test.go` becomes the bench source; the mode's `Run` shells out
  to `go test -bench` against it, same pattern as `zap_vs_codec`.
- Mode-specific flags: `--device=<blackwell|hopper|ampere>`, `--batch=1,10,100,1000`.
- Extras to surface: `kernel`, `ms_per_batch`, `throughput_gops`.

Until ported, run the legacy bench out of `/tmp/spark-gpu-bench/`.
