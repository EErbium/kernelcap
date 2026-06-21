package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	PollInterval    time.Duration `json:"poll_interval" yaml:"poll_interval"`
	ProcRoot        string        `json:"proc_root" yaml:"proc_root"`
	SysRoot         string        `json:"sys_root" yaml:"sys_root"`
	HTTPListenAddr  string        `json:"http_listen_addr" yaml:"http_listen_addr"`
	EnableBPF       bool          `json:"enable_bpf" yaml:"enable_bpf"`
	BpfProgPath     string        `json:"bpf_prog_path,omitempty" yaml:"bpf_prog_path,omitempty"`
	Verbose         bool          `json:"verbose" yaml:"verbose"`

	ProxyEnabled    bool   `json:"proxy_enabled" yaml:"proxy_enabled"`
	ProxyListenAddr string `json:"proxy_listen_addr" yaml:"proxy_listen_addr"`
	ProxyLogFilePath string `json:"proxy_log_file,omitempty" yaml:"proxy_log_file,omitempty"`

	AgentID          string `json:"agent_id" yaml:"agent_id"`
	UpstreamEndpoint string `json:"upstream_endpoint" yaml:"upstream_endpoint"`
	AuthToken        string `json:"auth_token" yaml:"auth_token"`
	RingBufferMaxMB  int    `json:"ring_buffer_max_mb" yaml:"ring_buffer_max_mb"`

	DashboardAddr    string `json:"dashboard_addr" yaml:"dashboard_addr"`
	DetectorEnabled  bool   `json:"detector_enabled" yaml:"detector_enabled"`
	ProfilerEnabled  bool   `json:"profiler_enabled" yaml:"profiler_enabled"`

	MitigationEnabled   bool          `json:"mitigation_enabled" yaml:"mitigation_enabled"`
	MitigationWhitelist []string      `json:"mitigation_whitelist,omitempty" yaml:"mitigation_whitelist,omitempty"`
	DockerSocketPath    string        `json:"docker_socket_path,omitempty" yaml:"docker_socket_path,omitempty"`
	DockerAPITimeout    time.Duration `json:"docker_api_timeout,omitempty" yaml:"docker_api_timeout,omitempty"`

	RouterEnabled           bool          `json:"router_enabled" yaml:"router_enabled"`
	RouterTokenCap          int           `json:"router_token_cap" yaml:"router_token_cap"`
	RouterKeepRecent        int           `json:"router_keep_recent" yaml:"router_keep_recent"`
	RouterFallbackEndpoint  string        `json:"router_fallback_endpoint" yaml:"router_fallback_endpoint"`
	RouterFallbackModel     string        `json:"router_fallback_model" yaml:"router_fallback_model"`
	RouterFallbackAuthToken string        `json:"router_fallback_auth_token" yaml:"router_fallback_auth_token"`
	RouterCoolingOffSeconds int           `json:"router_cooling_off_seconds" yaml:"router_cooling_off_seconds"`

	PolicyMaxActionsPerPID  int           `json:"policy_max_actions_per_pid" yaml:"policy_max_actions_per_pid"`
	PolicyVelocityWindowMin int           `json:"policy_velocity_window_min" yaml:"policy_velocity_window_min"`
	PolicyCooldownSeconds   int           `json:"policy_cooldown_seconds" yaml:"policy_cooldown_seconds"`
	PolicyLedgerMaxEntries  int           `json:"policy_ledger_max_entries" yaml:"policy_ledger_max_entries"`
	PolicyPidThreshold      int           `json:"policy_pid_threshold" yaml:"policy_pid_threshold"`
}

func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	return &Config{
		PollInterval:     500 * time.Millisecond,
		ProcRoot:         "/proc",
		SysRoot:          "/sys",
		HTTPListenAddr:   ":9090",
		EnableBPF:        true,
		Verbose:          false,
		ProxyEnabled:     false,
		ProxyListenAddr:  ":9999",
		AgentID:          hostname,
		RingBufferMaxMB:  50,
		DashboardAddr:    "127.0.0.1:8088",
		DetectorEnabled:  true,
		ProfilerEnabled:  true,

		MitigationEnabled: false,
		DockerSocketPath:  "unix:///var/run/docker.sock",
		DockerAPITimeout:  5 * time.Second,

		RouterEnabled:           false,
		RouterTokenCap:          20,
		RouterKeepRecent:        10,
		RouterFallbackEndpoint:  "http://127.0.0.1:8000/v1/chat/completions",
		RouterFallbackModel:     "meta-llama/Llama-3-8b-Instruct",
		RouterCoolingOffSeconds: 60,

		PolicyMaxActionsPerPID:  5,
		PolicyVelocityWindowMin: 10,
		PolicyCooldownSeconds:   60,
		PolicyLedgerMaxEntries:  10000,
		PolicyPidThreshold:      100,
	}
}

func Load() (*Config, error) {
	cfg := DefaultConfig()

	cfgFile := os.Getenv("PROFILER_CONFIG")
	if cfgFile != "" {
		if err := loadConfigFile(cfgFile, cfg); err != nil {
			return nil, fmt.Errorf("load config file: %w", err)
		}
	}

	if v := os.Getenv("PROFILER_POLL_INTERVAL_MS"); v != "" {
		ms, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PROFILER_POLL_INTERVAL_MS: %s", v)
		}
		cfg.PollInterval = time.Duration(ms) * time.Millisecond
	}
	if v := os.Getenv("PROFILER_PROC_ROOT"); v != "" {
		cfg.ProcRoot = v
	}
	if v := os.Getenv("PROFILER_HTTP_ADDR"); v != "" {
		cfg.HTTPListenAddr = v
	}
	if v := os.Getenv("PROFILER_ENABLE_BPF"); v != "" {
		cfg.EnableBPF = v == "true"
	}
	if v := os.Getenv("PROFILER_VERBOSE"); v != "" {
		cfg.Verbose = v == "true"
	}
	if v := os.Getenv("PROFILER_PROXY_ENABLED"); v != "" {
		cfg.ProxyEnabled = v == "true"
	}
	if v := os.Getenv("PROFILER_PROXY_ADDR"); v != "" {
		cfg.ProxyListenAddr = v
	}
	if v := os.Getenv("PROFILER_PROXY_LOG_FILE"); v != "" {
		cfg.ProxyLogFilePath = v
	}

	if v := os.Getenv("AGENT_ID"); v != "" {
		cfg.AgentID = v
	}
	if v := os.Getenv("UPSTREAM_ENDPOINT"); v != "" {
		cfg.UpstreamEndpoint = v
	}
	if v := os.Getenv("AUTH_TOKEN"); v != "" {
		cfg.AuthToken = v
	}
	if v := os.Getenv("RING_BUFFER_MAX_MB"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			cfg.RingBufferMaxMB = n
		}
	}

	if v := os.Getenv("PROFILER_DASHBOARD_ADDR"); v != "" {
		cfg.DashboardAddr = v
	}
	if v := os.Getenv("PROFILER_DETECTOR_ENABLED"); v != "" {
		cfg.DetectorEnabled = v == "true"
	}
	if v := os.Getenv("PROFILER_PROFILER_ENABLED"); v != "" {
		cfg.ProfilerEnabled = v == "true"
	}

	if v := os.Getenv("PROFILER_MITIGATION_ENABLED"); v != "" {
		cfg.MitigationEnabled = v == "true"
	}
	if v := os.Getenv("PROFILER_DOCKER_SOCKET"); v != "" {
		cfg.DockerSocketPath = v
	}
	if v := os.Getenv("PROFILER_DOCKER_TIMEOUT_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			cfg.DockerAPITimeout = time.Duration(ms) * time.Millisecond
		}
	}

	if v := os.Getenv("PROFILER_ROUTER_ENABLED"); v != "" {
		cfg.RouterEnabled = v == "true"
	}
	if v := os.Getenv("PROFILER_ROUTER_FALLBACK_ENDPOINT"); v != "" {
		cfg.RouterFallbackEndpoint = v
	}
	if v := os.Getenv("PROFILER_ROUTER_FALLBACK_MODEL"); v != "" {
		cfg.RouterFallbackModel = v
	}
	if v := os.Getenv("PROFILER_ROUTER_COOLING_OFF_SECONDS"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			cfg.RouterCoolingOffSeconds = s
		}
	}

	if v := os.Getenv("PROFILER_POLICY_MAX_ACTIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PolicyMaxActionsPerPID = n
		}
	}
	if v := os.Getenv("PROFILER_POLICY_VELOCITY_WINDOW_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PolicyVelocityWindowMin = n
		}
	}
	if v := os.Getenv("PROFILER_POLICY_COOLDOWN_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PolicyCooldownSeconds = n
		}
	}

	return cfg, nil
}

func loadConfigFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("parse JSON %s: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("parse YAML %s: %w", path, err)
		}
	default:
		return fmt.Errorf("unsupported config file format: %s (use .json, .yaml, or .yml)", ext)
	}
	return nil
}
