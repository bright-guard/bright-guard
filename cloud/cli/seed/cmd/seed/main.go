package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"text/tabwriter"

	"github.com/bright-guard/bright-guard/cloud/cli/seed/internal/client"
	"github.com/bright-guard/bright-guard/cloud/cli/seed/internal/fixture"
	"github.com/bright-guard/bright-guard/cloud/cli/seed/internal/seeder"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "bg-seed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("bg-seed", flag.ContinueOnError)
	fs.Usage = func() {
		w := fs.Output()
		fmt.Fprintf(w, "Usage: bg-seed [flags]\n\n")
		fmt.Fprintf(w, "Seeds a Bright Guard org with demo gateways, MCP servers, and invocations.\n\n")
		fmt.Fprintf(w, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(w, "\nAuth (exactly one):\n")
		fmt.Fprintf(w, "  --token=<cli token>          Sent as Authorization: Bearer (preferred)\n")
		fmt.Fprintf(w, "  --cookie=<bg_session value>  Sent as Cookie: bg_session=...  (local dev only)\n")
		fmt.Fprintf(w, "  BG_COOKIE env var is used if --cookie is not set.\n")
	}

	controlPlane := fs.String("control-plane", "", "Control plane base URL (required)")
	token := fs.String("token", "", "CLI bearer token (bg_cli_<uuid>.<secret>)")
	cookie := fs.String("cookie", "", "Session cookie value (bg_session=...)")
	fixturePath := fs.String("fixture", "fixtures/acme.yaml", "Path to fixture YAML")
	orgName := fs.String("org-name", "", "Override fixture.org.name")
	seedFlag := fs.Int64("seed", 42, "RNG seed for deterministic invocations")
	batchSize := fs.Int("batch-size", 200, "Max invocations per observations request")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *controlPlane == "" {
		fs.Usage()
		return fmt.Errorf("--control-plane is required")
	}
	if *cookie == "" {
		*cookie = os.Getenv("BG_COOKIE")
	}

	c, err := client.New(*controlPlane, *token, *cookie)
	if err != nil {
		return err
	}

	fx, err := fixture.Load(*fixturePath)
	if err != nil {
		return err
	}

	logger := log.New(os.Stderr, "[seed] ", log.LstdFlags)
	summary, err := seeder.Run(c, seeder.Options{
		Fixture:   fx,
		OrgName:   *orgName,
		RNGSeed:   *seedFlag,
		Logger:    logger,
		BatchSize: *batchSize,
	})
	if err != nil {
		return err
	}
	printSummary(os.Stdout, summary)
	return nil
}

func printSummary(out io.Writer, s *seeder.Summary) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Seeded org: %s (%s)\n", s.OrgName, s.OrgID)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "GATEWAY\tID\tSERVERS\tCAPS\tINVOCATIONS")
	totalSrv, totalCap, totalInv := 0, 0, 0
	for _, g := range s.GatewayResults {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\n", g.Name, shortID(g.ID), g.Servers, g.Capabilities, g.Invocations)
		totalSrv += g.Servers
		totalCap += g.Capabilities
		totalInv += g.Invocations
	}
	fmt.Fprintf(tw, "TOTAL\t\t%d\t%d\t%d\n", totalSrv, totalCap, totalInv)
	tw.Flush()
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
