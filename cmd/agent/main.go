package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/anomalyco/ai-compute-profiler/internal/proxy/config"
	"github.com/anomalyco/ai-compute-profiler/internal/proxy/engine"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	if len(os.Args) < 2 || os.Args[1] == "help" || os.Args[1] == "--help" || os.Args[1] == "-h" {
		printUsage()
		return
	}

	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `AI Compute Profiler Agent

Usage:
  profiler-agent run [flags]

Flags:
  -config string
        Path to config file (JSON, YAML, or YML)

Environment Variables:
  PROFILER_CONFIG             Path to config file
  PROFILER_POLL_INTERVAL_MS   Polling interval in milliseconds
  PROFILER_HTTP_ADDR          Metrics HTTP listen address (default ":9090")
  PROFILER_ENABLE_BPF         Enable eBPF process tracking (default true)
  PROFILER_PROXY_ENABLED      Enable AI API proxy (default false)
  PROFILER_PROXY_ADDR         Proxy listen address (default ":9999")
  PROFILER_DASHBOARD_ADDR     Dashboard API address (default "127.0.0.1:8088")
  PROFILER_DETECTOR_ENABLED   Enable semantic deadlock detector (default true)
  PROFILER_PROFILER_ENABLED   Enable memory leak/idle GPU profiler (default true)
  AGENT_ID                    Node identifier (default: hostname)
  UPSTREAM_ENDPOINT           Upstream URL for telemetry data
  AUTH_TOKEN                  Bearer token for upstream authentication
  RING_BUFFER_MAX_MB          In-memory ring buffer size in MB (default 50)
`)
}

func runCmd(args []string) {
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := flags.String("config", "", "Path to config file")
	flags.Parse(args)

	if *configPath != "" {
		os.Setenv("PROFILER_CONFIG", *configPath)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	log.Printf("starting agent: agent_id=%s poll=%v http=%s bpf=%v proxy=%v upstream=%s ring_buffer=%dMB dashboard=%s detector=%v profiler=%v",
		cfg.AgentID, cfg.PollInterval, cfg.HTTPListenAddr, cfg.EnableBPF,
		cfg.ProxyEnabled, maskToken(cfg.UpstreamEndpoint, cfg.AuthToken), cfg.RingBufferMaxMB,
		cfg.DashboardAddr, cfg.DetectorEnabled, cfg.ProfilerEnabled)

	eng := engine.New(cfg, log.Printf)
	ctx := context.Background()
	if err := eng.Start(ctx); err != nil {
		log.Fatalf("engine error: %v", err)
	}
}

func maskToken(endpoint, token string) string {
	if token == "" {
		return endpoint
	}
	if len(token) <= 8 {
		return endpoint + " [token: ***]"
	}
	return endpoint + fmt.Sprintf(" [token: %s***%s]", token[:4], token[len(token)-4:])
}
