// Package bench declares the Mode interface every sub-benchmark satisfies.
// The luxbench CLI dispatches --mode=<name> to a registered Mode.
//
// Adding a new bench: implement Mode in modes/<name>/, then register it
// in cmd/luxbench/main.go. That's the only edit outside modes/<name>/.
package bench

import (
	"context"
	"flag"

	"github.com/luxfi/bench/result"
)

// Mode is the contract between cmd/luxbench and every modes/* package.
type Mode interface {
	// Name returns the --mode value, e.g. "zap-vs-codec". Stable;
	// downstream JSON consumers key on this.
	Name() string

	// Description is one line, printed under -help.
	Description() string

	// RegisterFlags binds mode-specific flags to fs. The CLI parses
	// --mode first, then re-parses with the mode's flags bound.
	RegisterFlags(fs *flag.FlagSet)

	// Run executes the bench. Common flags (--benchtime, --count,
	// --output) are passed via Config.
	Run(ctx context.Context, cfg Config) ([]result.ResultRecord, error)
}

// Config carries the cross-mode flags every Run gets.
type Config struct {
	// BenchTime is the `go test -benchtime` value (e.g. "2s", "10x").
	// Modes that don't use `go test` may ignore it.
	BenchTime string
	// Count is the `go test -count` value (i.e. how many times each
	// subtest re-runs). Modes that don't use `go test` may ignore it.
	Count int
	// Output is the path the CLI will write the JSON file to. Modes
	// may use it for relative paths (e.g. side artifacts) but writing
	// the canonical JSON is the CLI's job, not the Mode's.
	Output string
	// Verbose enables per-mode verbose output (stdout streaming of
	// underlying `go test -v -bench`).
	Verbose bool
}
