// Command luxbench is the unified benchmark driver for the luxfi stack.
//
// One harness, one way:
//
//	luxbench --mode=zap-vs-codec --benchtime=2s --count=3 --output=/tmp/x.json
//	luxbench --mode=gpu          --device=blackwell --batch=1,10,100,1000 --output=/tmp/x.json
//	luxbench --mode=multi-validator --scale=100 --profile=hybrid-1s --output=/tmp/x.json
//	luxbench --mode=parity       --scheme=corona,pulsar --output=/tmp/x.json
//	luxbench --mode=matrix       --profiles=all --validators=64 --output=/tmp/x.json
//
// Adding a new mode:
//  1. Implement bench.Mode in modes/<name>/.
//  2. Register it in main() below.
//  3. Document mode-specific flags in ../../README.md.
//
// Result file format: see ../../result/schema.go (result.File envelope).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	bench "github.com/luxfi/bench"
	"github.com/luxfi/bench/result"

	// Mode registrations: keep this list alphabetical so `luxbench -help`
	// prints in stable order.
	zapvscodec "github.com/luxfi/bench/modes/zap_vs_codec"
)

// modes is the global mode registry. Populated in main() (not init) so
// the registration order is explicit and visible.
var modes = map[string]bench.Mode{}

func register(m bench.Mode) {
	if _, dup := modes[m.Name()]; dup {
		panic(fmt.Sprintf("duplicate mode: %q", m.Name()))
	}
	modes[m.Name()] = m
}

func main() {
	register(zapvscodec.New())
	// TODO modes (Phase 2-5) — left intentionally unwired until ported:
	//   register(gpu.New())
	//   register(multivalidator.New())
	//   register(parity.New())
	//   register(matrix.New())

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "luxbench:", err)
		os.Exit(1)
	}
}

// run is the testable entry. It parses flags in two passes so each mode
// can register its own flag set without colliding with the common ones.
func run(argv []string) error {
	common := flag.NewFlagSet("luxbench", flag.ContinueOnError)
	common.SetOutput(os.Stderr)

	var (
		modeName  string
		benchTime string
		count     int
		output    string
		verbose   bool
		listOnly  bool
	)
	common.StringVar(&modeName, "mode", "", "bench mode (required); see -list")
	common.StringVar(&benchTime, "benchtime", "1s", "go test -benchtime value")
	common.IntVar(&count, "count", 1, "go test -count value (runs per subtest)")
	common.StringVar(&output, "output", "", "output JSON file (required when --mode set)")
	common.BoolVar(&verbose, "verbose", false, "stream underlying tool output")
	common.BoolVar(&listOnly, "list", false, "list registered modes and exit")

	common.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: luxbench --mode=<name> [--benchtime=Xs] [--count=N] --output=PATH [mode-flags...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Common flags:")
		common.PrintDefaults()
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Registered modes:")
		printModes(os.Stderr)
	}

	// First pass: parse only the common flags, stop at the first unknown.
	// We do this by hand because Go's flag pkg doesn't natively support
	// "parse some, leave the rest" mid-stream.
	common.SetOutput(os.Stderr)
	if err := parseUntilUnknown(common, argv); err != nil {
		return err
	}
	if listOnly {
		printModes(os.Stdout)
		return nil
	}
	if modeName == "" {
		common.Usage()
		return fmt.Errorf("--mode is required")
	}
	mode, ok := modes[modeName]
	if !ok {
		return fmt.Errorf("unknown mode %q; run `luxbench -list`", modeName)
	}
	if output == "" {
		return fmt.Errorf("--output is required (path to JSON result file)")
	}

	// Second pass: rebuild the flag set, add mode flags, parse all args.
	all := flag.NewFlagSet("luxbench", flag.ContinueOnError)
	all.SetOutput(os.Stderr)
	all.StringVar(&modeName, "mode", modeName, "")
	all.StringVar(&benchTime, "benchtime", benchTime, "")
	all.IntVar(&count, "count", count, "")
	all.StringVar(&output, "output", output, "")
	all.BoolVar(&verbose, "verbose", verbose, "")
	all.BoolVar(&listOnly, "list", listOnly, "")
	mode.RegisterFlags(all)
	if err := all.Parse(argv); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := bench.Config{
		BenchTime: benchTime,
		Count:     count,
		Output:    output,
		Verbose:   verbose,
	}
	records, err := mode.Run(ctx, cfg)
	if err != nil {
		return fmt.Errorf("mode %s: %w", mode.Name(), err)
	}
	if err := result.WriteFile(output, mode.Name(), records); err != nil {
		return err
	}
	fmt.Printf("luxbench: wrote %d records to %s\n", len(records), output)
	return nil
}

// parseUntilUnknown is a tolerant first pass: it parses what it knows and
// stops at the first flag it doesn't recognize, leaving the rest for the
// mode's flag set to consume.
func parseUntilUnknown(fs *flag.FlagSet, argv []string) error {
	// We can't ask flag.FlagSet to ignore unknowns directly, so we use a
	// custom error handler.
	known := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { known[f.Name] = true })
	// Build a filtered argv: keep only flags fs knows.
	filtered := []string{}
	i := 0
	for i < len(argv) {
		a := argv[i]
		if !strings.HasPrefix(a, "-") {
			i++
			continue
		}
		// Normalize "-foo" / "--foo".
		name := strings.TrimLeft(a, "-")
		eq := strings.IndexByte(name, '=')
		if eq >= 0 {
			name = name[:eq]
		}
		if known[name] {
			filtered = append(filtered, a)
			if eq < 0 {
				// Next arg may be value if not bool. We can't tell
				// for sure without inspecting the flag's type, so
				// we conservatively pull the next non-flag arg.
				if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
					filtered = append(filtered, argv[i+1])
					i += 2
					continue
				}
			}
			i++
			continue
		}
		// Unknown flag; skip without consuming.
		i++
		if i < len(argv) && !strings.HasPrefix(argv[i], "-") {
			i++ // skip possible value
		}
	}
	return fs.Parse(filtered)
}

// printModes lists registered modes alphabetically with one-line descriptions.
func printModes(w *os.File) {
	names := make([]string, 0, len(modes))
	for n := range modes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-20s  %s\n", n, modes[n].Description())
	}
}
