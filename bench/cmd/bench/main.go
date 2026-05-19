// Command bench runs the AgentShield vs naive integration comparison harness.
//
// Usage:
//
//	go run ./bench/cmd/bench -mode all -n 50 -out results.csv
//
// Flags:
//
//	-mode    comma-separated list of scenarios: garbage,brownout,down (default "all")
//	-n       number of requests per path per scenario (default 50)
//	-out     path for the CSV output (default "bench/results.csv")
//	-md      path for the Markdown table output (default "bench/results.md")
//	-seed    RNG seed for deterministic fake backend (default 42)
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yabanci/agentshield/bench/fakebackend"
	"github.com/yabanci/agentshield/bench/runner"
)

var benchPrompts = []string{
	"Explain how a circuit breaker works in distributed systems.",
	"What is the difference between concurrency and parallelism in Go?",
	"Describe the CAP theorem and its practical implications.",
	"How does semantic search differ from keyword search?",
	"What are the main failure modes of LLM APIs in production?",
	"Explain the concept of backpressure in streaming systems.",
	"What is an LRU cache and when would you use one?",
	"Describe how JWT authentication works end to end.",
}

func main() {
	modeFlag := flag.String("mode", "all", "comma-separated scenarios: garbage,brownout,down or all")
	nFlag := flag.Int("n", 50, "requests per path per scenario")
	outFlag := flag.String("out", "bench/results.csv", "CSV output path")
	mdFlag := flag.String("md", "bench/results.md", "Markdown output path")
	seedFlag := flag.Int64("seed", 42, "RNG seed for deterministic fake backend")
	flag.Parse()

	scenarios := parseMode(*modeFlag)
	if len(scenarios) == 0 {
		log.Fatal("no valid scenarios specified")
	}

	log.Printf("bench: seed=%d, n=%d, scenarios=%v", *seedFlag, *nFlag, scenarios)

	// Start fake backend once, shared across all scenarios.
	// A fresh server per scenario would reset the RNG, breaking scenario isolation;
	// sharing it means each scenario's requests fall into the global RNG stream.
	srv := fakebackend.New(*seedFlag)
	defer srv.Close()
	log.Printf("bench: fake backend at %s", srv.URL())

	// Warm up the semantic cache for AgentShield by sending 3 good requests
	// (no scenario header) before the first scenario starts.
	// This simulates a production deployment where some cache entries already
	// exist. It also gives the QualityEvaluator's rolling-length window a
	// baseline before the bad responses start arriving.
	log.Printf("bench: warming up cache with 3 good requests...")
	if err := warmup(srv.URL(), 3); err != nil {
		log.Printf("bench: warmup error (non-fatal): %v", err)
	}

	results := make([]runner.ScenarioResult, 0, len(scenarios))
	for _, scenario := range scenarios {
		log.Printf("bench: running scenario %q (%d requests per path)...", scenario, *nFlag)
		start := time.Now()
		r := runner.RunScenario(scenario, srv.URL(), *nFlag, benchPrompts)
		elapsed := time.Since(start)
		results = append(results, r)
		log.Printf("bench: scenario %q done in %s", scenario, elapsed.Round(time.Millisecond))
		logScenarioSummary(r)
	}

	// Write CSV.
	if err := writeCSV(*outFlag, results); err != nil {
		log.Fatalf("bench: write CSV: %v", err)
	}
	log.Printf("bench: wrote %s", *outFlag)

	// Write Markdown.
	if err := writeMarkdown(*mdFlag, results); err != nil {
		log.Fatalf("bench: write Markdown: %v", err)
	}
	log.Printf("bench: wrote %s", *mdFlag)

	printSummaryTable(results)
}

func parseMode(mode string) []string {
	if mode == "all" {
		return []string{runner.ScenarioGarbage, runner.ScenarioBrownout, runner.ScenarioDown}
	}
	parts := strings.Split(mode, ",")
	out := make([]string, 0, len(parts))
	valid := map[string]bool{
		runner.ScenarioGarbage:  true,
		runner.ScenarioBrownout: true,
		runner.ScenarioDown:     true,
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if valid[p] {
			out = append(out, p)
		} else {
			log.Printf("bench: ignoring unknown scenario %q", p)
		}
	}
	return out
}

func warmup(baseURL string, n int) error {
	c := runner.NewAgentShieldClient(baseURL, "")
	for i := 0; i < n; i++ {
		ctx := runner.BackgroundCtx()
		_, _, err := c.Generate(ctx, benchPrompts[i%len(benchPrompts)])
		if err != nil {
			return err
		}
	}
	return nil
}

func logScenarioSummary(r runner.ScenarioResult) {
	log.Printf("  naive    success=%.0f%% useful=%.0f%% p50=%dms p95=%dms",
		r.Naive.Stats.SuccessRate*100, r.Naive.Stats.UsefulRate*100,
		r.Naive.Stats.P50MS, r.Naive.Stats.P95MS,
	)
	log.Printf("  shield   success=%.0f%% useful=%.0f%% p50=%dms p95=%dms",
		r.Shield.Stats.SuccessRate*100, r.Shield.Stats.UsefulRate*100,
		r.Shield.Stats.P50MS, r.Shield.Stats.P95MS,
	)
}

func writeCSV(path string, results []runner.ScenarioResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path) //nolint:gosec // bench output file — path from flag
	if err != nil {
		return err
	}

	w := csv.NewWriter(f)
	_ = w.Write([]string{
		"scenario", "path", "request_num",
		"latency_ms", "success", "useful", "quality_score", "tier",
	})

	for _, r := range results {
		for i, s := range r.Naive.Samples {
			_ = w.Write(sampleRow(r.Scenario, "naive", i+1, s))
		}
		for i, s := range r.Shield.Samples {
			_ = w.Write(sampleRow(r.Scenario, "agentshield", i+1, s))
		}
	}
	w.Flush()
	if werr := w.Error(); werr != nil {
		_ = f.Close()
		return werr
	}
	return f.Close()
}

func sampleRow(scenario, path string, n int, s runner.Sample) []string {
	success := "0"
	if s.Success {
		success = "1"
	}
	useful := "0"
	if s.Useful {
		useful = "1"
	}
	return []string{
		scenario, path, fmt.Sprintf("%d", n),
		fmt.Sprintf("%d", s.LatencyMS),
		success, useful,
		fmt.Sprintf("%.3f", s.Score),
		s.Tier,
	}
}

func writeMarkdown(path string, results []runner.ScenarioResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path) //nolint:gosec // bench output file — path from flag
	if err != nil {
		return err
	}

	w := f

	_, _ = fmt.Fprintln(w, "# AgentShield vs Naive Integration — Benchmark Results")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "> Generated by `go run ./bench/cmd/bench`. See [bench/README.md](README.md) for methodology.")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "## Results")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "| Scenario | Path | Success% | Useful% | p50 (ms) | p95 (ms) | p99 (ms) | StdDev (ms) | First Useful (ms) |")
	_, _ = fmt.Fprintln(w, "|----------|------|----------|---------|----------|----------|----------|-------------|-------------------|")

	for _, r := range results {
		writeResultRow(w, r.Scenario, "naive", r.Naive.Stats)
		writeResultRow(w, r.Scenario, "agentshield", r.Shield.Stats)
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "## Key Observations")
	_, _ = fmt.Fprintln(w)
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "**%s**: naive useful rate %.0f%% → agentshield useful rate %.0f%%",
			r.Scenario,
			r.Naive.Stats.UsefulRate*100,
			r.Shield.Stats.UsefulRate*100,
		)
		delta := r.Shield.Stats.UsefulRate - r.Naive.Stats.UsefulRate
		if delta > 0 {
			_, _ = fmt.Fprintf(w, " (+%.0f pp)", delta*100)
		}
		_, _ = fmt.Fprintln(w, ".")
		_, _ = fmt.Fprintln(w)
	}

	_, _ = fmt.Fprintln(w, "---")
	_, _ = fmt.Fprintf(w, "_Run at %s_\n", time.Now().UTC().Format(time.RFC3339))
	return f.Close()
}

func writeResultRow(w *os.File, scenario, path string, s runner.Stats) {
	firstUseful := fmt.Sprintf("%d", s.TimeToFirstUsefulMS)
	if s.TimeToFirstUsefulMS < 0 {
		firstUseful = "N/A"
	}
	_, _ = fmt.Fprintf(w, "| %-8s | %-11s | %7.0f%% | %6.0f%% | %8d | %8d | %8d | %11.0f | %17s |\n",
		scenario, path,
		s.SuccessRate*100, s.UsefulRate*100,
		s.P50MS, s.P95MS, s.P99MS,
		s.StdDevMS,
		firstUseful,
	)
}

func printSummaryTable(results []runner.ScenarioResult) {
	fmt.Println()
	fmt.Println("=== BENCH SUMMARY ===")
	fmt.Printf("%-10s  %-12s  %9s  %8s  %9s  %9s\n",
		"SCENARIO", "PATH", "SUCCESS%", "USEFUL%", "P50(ms)", "P95(ms)")
	fmt.Println(strings.Repeat("-", 65))
	for _, r := range results {
		printRow(r.Scenario, "naive", r.Naive.Stats)
		printRow(r.Scenario, "agentshield", r.Shield.Stats)
	}
	fmt.Println()
}

func printRow(scenario, path string, s runner.Stats) {
	fmt.Printf("%-10s  %-12s  %8.0f%%  %7.0f%%  %9d  %9d\n",
		scenario, path,
		s.SuccessRate*100, s.UsefulRate*100,
		s.P50MS, s.P95MS,
	)
}
