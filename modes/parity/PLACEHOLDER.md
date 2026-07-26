# modes/parity — TODO (Phase 4)

Port `~/work/lux/threshold/protocols/parity/parity_test.go` to a
mode wrapper here (the source bench stays in `lux/threshold/protocols/parity`
since it tests in-tree code).

- Mode-specific flags: `--scheme=corona,pulsar`, `--vectors=<path>`.
- The mode shells out to `go test -run TestParity` in the threshold pkg
  and parses its output (the parity tests already emit structured logs).
- Extras to surface: `scheme_a`, `scheme_b`, `diff_count`, `vectors_checked`.

Goal: catch parity drift between alternative implementations of the
same cryptographic primitive without needing a separate harness.
