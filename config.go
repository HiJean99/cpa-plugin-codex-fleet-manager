package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	// PluginID is the stable CPA and Store identifier. It deliberately stays
	// separate from the human-facing product name so this plugin can coexist
	// with its predecessor without sharing routes or persisted state.
	PluginID          = "codex-fleet-manager"
	PluginDisplayName = "Codex Fleet Manager"

	chatGPTQuotaEndpoint = "https://chatgpt.com/backend-api/wham/usage"

	MonthlyModePriority    MonthlyMode = "priority"
	MonthlyModeExpiryOrder MonthlyMode = "expiry_order"

	FallbackFillFirst FallbackMode = "fill-first"
)

var pluginVersion = "0.1.1-mingli.1"

type MonthlyMode string

type FallbackMode string

type Config struct {
	HandleEnabled        bool
	QuotaRefreshInterval time.Duration
	// StaleAfter classifies cache age and permits pick-time recovery. It does
	// not own a normal-refresh deadline.
	StaleAfter                      time.Duration
	MonthlyMode                     MonthlyMode
	Fallback                        FallbackMode
	EnableUsageFeedback             bool
	EnableResetProbe                bool
	ResetProbeModel                 string
	ResetProbeRequireResetAtSlide   bool
	ResetProbeObservationInterval   time.Duration
	ResetProbeDriftThreshold        time.Duration
	ResetProbeMinInterval           time.Duration
	ResetProbeFailureCooldown       time.Duration
	ProbeOnProvisionalRoster        bool
	MaxRefreshConcurrency           int
	QuotaEndpoint                   string
	RefreshActiveWindow             time.Duration
	RefreshAfterResetDelay          time.Duration
	RefreshRetryDelays              []time.Duration
	RefreshOnStartup                bool
	CircuitFailureThreshold         int
	CircuitOpenDuration             time.Duration
	CircuitHalfOpenSuccessThreshold int
	MaxLogEntries                   int
	LogRetention                    time.Duration
}

type registration struct {
	SchemaVersion uint32                   `json:"schema_version"`
	Metadata      pluginapi.Metadata       `json:"metadata"`
	Capabilities  registrationCapabilities `json:"capabilities"`
}

type registrationCapabilities struct {
	Scheduler     bool `json:"scheduler"`
	UsagePlugin   bool `json:"usage_plugin"`
	ManagementAPI bool `json:"management_api"`
}

type rawConfig struct {
	HandleEnabled                   *bool  `yaml:"handle_enabled"`
	QuotaRefreshInterval            string `yaml:"quota_refresh_interval"`
	StaleAfter                      string `yaml:"stale_after"`
	MonthlyMode                     string `yaml:"monthly_mode"`
	Fallback                        string `yaml:"fallback"`
	EnableUsageFeedback             *bool  `yaml:"enable_usage_feedback"`
	EnableResetProbe                *bool  `yaml:"enable_reset_probe"`
	ResetProbeModel                 string `yaml:"reset_probe_model"`
	ResetProbeRequireResetAtSlide   *bool  `yaml:"reset_probe_require_reset_at_slide"`
	ResetProbeObservationInterval   string `yaml:"reset_probe_observation_interval"`
	ResetProbeDriftThreshold        string `yaml:"reset_probe_drift_threshold"`
	ResetProbeMinInterval           string `yaml:"reset_probe_min_interval"`
	ResetProbeFailureCooldown       string `yaml:"reset_probe_failure_cooldown"`
	ProbeOnProvisionalRoster        *bool  `yaml:"probe_on_provisional_roster"`
	MaxRefreshConcurrency           *int   `yaml:"max_refresh_concurrency"`
	QuotaEndpoint                   string `yaml:"quota_endpoint"`
	RefreshActiveWindow             string `yaml:"refresh_active_window"`
	RefreshAfterResetDelay          string `yaml:"refresh_after_reset_delay"`
	RefreshRetryDelays              string `yaml:"refresh_retry_delays"`
	RefreshOnStartup                *bool  `yaml:"refresh_on_startup"`
	CircuitFailureThreshold         *int   `yaml:"circuit_failure_threshold"`
	CircuitOpenDuration             string `yaml:"circuit_open_duration"`
	CircuitHalfOpenSuccessThreshold *int   `yaml:"circuit_half_open_success_threshold"`
	MaxLogEntries                   *int   `yaml:"max_log_entries"`
	LogRetention                    string `yaml:"log_retention"`
}

func DefaultConfig() Config {
	return Config{
		HandleEnabled:                   true,
		QuotaRefreshInterval:            30 * time.Minute,
		StaleAfter:                      5 * time.Hour,
		MonthlyMode:                     MonthlyModeExpiryOrder,
		Fallback:                        FallbackFillFirst,
		EnableUsageFeedback:             true,
		EnableResetProbe:                false,
		ResetProbeModel:                 "gpt-5.5",
		ResetProbeRequireResetAtSlide:   false,
		ResetProbeObservationInterval:   30 * time.Minute,
		ResetProbeDriftThreshold:        2 * time.Minute,
		ResetProbeMinInterval:           10 * time.Minute,
		ResetProbeFailureCooldown:       10 * time.Minute,
		MaxRefreshConcurrency:           1,
		QuotaEndpoint:                   chatGPTQuotaEndpoint,
		RefreshActiveWindow:             time.Hour,
		RefreshAfterResetDelay:          time.Minute,
		RefreshRetryDelays:              []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		RefreshOnStartup:                false,
		CircuitFailureThreshold:         5,
		CircuitOpenDuration:             30 * time.Minute,
		CircuitHalfOpenSuccessThreshold: 2,
		MaxLogEntries:                   200,
		LogRetention:                    24 * time.Hour,
	}
}

func NormalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.QuotaRefreshInterval <= 0 {
		cfg.QuotaRefreshInterval = defaults.QuotaRefreshInterval
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = defaults.StaleAfter
	}
	if cfg.MonthlyMode == "" {
		cfg.MonthlyMode = defaults.MonthlyMode
	}
	if cfg.Fallback == "" {
		cfg.Fallback = defaults.Fallback
	}
	if strings.TrimSpace(cfg.ResetProbeModel) == "" {
		cfg.ResetProbeModel = defaults.ResetProbeModel
	} else {
		cfg.ResetProbeModel = strings.TrimSpace(cfg.ResetProbeModel)
	}
	if cfg.ResetProbeObservationInterval <= 0 {
		cfg.ResetProbeObservationInterval = defaults.ResetProbeObservationInterval
	}
	if cfg.ResetProbeDriftThreshold <= 0 {
		cfg.ResetProbeDriftThreshold = defaults.ResetProbeDriftThreshold
	}
	if cfg.ResetProbeMinInterval <= 0 {
		cfg.ResetProbeMinInterval = defaults.ResetProbeMinInterval
	}
	if cfg.ResetProbeFailureCooldown <= 0 {
		cfg.ResetProbeFailureCooldown = defaults.ResetProbeFailureCooldown
	}
	if cfg.MaxRefreshConcurrency <= 0 {
		cfg.MaxRefreshConcurrency = defaults.MaxRefreshConcurrency
	}
	if strings.TrimSpace(cfg.QuotaEndpoint) == "" {
		cfg.QuotaEndpoint = defaults.QuotaEndpoint
	}
	if cfg.RefreshActiveWindow <= 0 {
		cfg.RefreshActiveWindow = defaults.RefreshActiveWindow
	}
	if cfg.RefreshAfterResetDelay <= 0 {
		cfg.RefreshAfterResetDelay = defaults.RefreshAfterResetDelay
	}
	cfg.RefreshRetryDelays = normalizeRetryDelays(cfg.RefreshRetryDelays, defaults.RefreshRetryDelays)
	if cfg.CircuitFailureThreshold <= 0 {
		cfg.CircuitFailureThreshold = defaults.CircuitFailureThreshold
	}
	if cfg.CircuitOpenDuration <= 0 {
		cfg.CircuitOpenDuration = defaults.CircuitOpenDuration
	}
	if cfg.CircuitHalfOpenSuccessThreshold <= 0 {
		cfg.CircuitHalfOpenSuccessThreshold = defaults.CircuitHalfOpenSuccessThreshold
	}
	if cfg.MaxLogEntries <= 0 {
		cfg.MaxLogEntries = defaults.MaxLogEntries
	}
	if cfg.LogRetention <= 0 {
		cfg.LogRetention = defaults.LogRetention
	}
	return cfg
}

func DecodeConfig(raw []byte) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) == 0 {
		return cfg, nil
	}

	var decoded rawConfig
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		return Config{}, err
	}
	if decoded.HandleEnabled != nil {
		cfg.HandleEnabled = *decoded.HandleEnabled
	}
	if decoded.QuotaRefreshInterval != "" {
		d, err := time.ParseDuration(decoded.QuotaRefreshInterval)
		if err != nil {
			return Config{}, fmt.Errorf("quota_refresh_interval: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("quota_refresh_interval must be positive")
		}
		cfg.QuotaRefreshInterval = d
	}
	if decoded.StaleAfter != "" {
		d, err := time.ParseDuration(decoded.StaleAfter)
		if err != nil {
			return Config{}, fmt.Errorf("stale_after: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("stale_after must be positive")
		}
		cfg.StaleAfter = d
	}
	if decoded.MonthlyMode != "" {
		cfg.MonthlyMode = MonthlyMode(decoded.MonthlyMode)
	}
	if cfg.MonthlyMode != MonthlyModePriority && cfg.MonthlyMode != MonthlyModeExpiryOrder {
		return Config{}, fmt.Errorf("monthly_mode must be %q or %q", MonthlyModePriority, MonthlyModeExpiryOrder)
	}
	if decoded.Fallback != "" {
		cfg.Fallback = FallbackMode(decoded.Fallback)
	}
	if cfg.Fallback != "" && cfg.Fallback != FallbackFillFirst {
		return Config{}, fmt.Errorf("fallback must be empty or %q", FallbackFillFirst)
	}
	if decoded.EnableUsageFeedback != nil {
		cfg.EnableUsageFeedback = *decoded.EnableUsageFeedback
	}
	if decoded.EnableResetProbe != nil {
		cfg.EnableResetProbe = *decoded.EnableResetProbe
	}
	if decoded.ResetProbeModel != "" {
		model, err := validateResetProbeModel(decoded.ResetProbeModel)
		if err != nil {
			return Config{}, err
		}
		cfg.ResetProbeModel = model
	}
	if decoded.ResetProbeRequireResetAtSlide != nil {
		cfg.ResetProbeRequireResetAtSlide = *decoded.ResetProbeRequireResetAtSlide
	}
	for _, value := range []struct {
		name string
		raw  string
		set  func(time.Duration)
	}{
		{"reset_probe_observation_interval", decoded.ResetProbeObservationInterval, func(d time.Duration) { cfg.ResetProbeObservationInterval = d }},
		{"reset_probe_drift_threshold", decoded.ResetProbeDriftThreshold, func(d time.Duration) { cfg.ResetProbeDriftThreshold = d }},
		{"reset_probe_min_interval", decoded.ResetProbeMinInterval, func(d time.Duration) { cfg.ResetProbeMinInterval = d }},
		{"reset_probe_failure_cooldown", decoded.ResetProbeFailureCooldown, func(d time.Duration) { cfg.ResetProbeFailureCooldown = d }},
	} {
		if value.raw == "" {
			continue
		}
		d, err := time.ParseDuration(value.raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", value.name, err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("%s must be positive", value.name)
		}
		value.set(d)
	}
	if decoded.ProbeOnProvisionalRoster != nil {
		cfg.ProbeOnProvisionalRoster = *decoded.ProbeOnProvisionalRoster
	}
	if decoded.MaxRefreshConcurrency != nil {
		if *decoded.MaxRefreshConcurrency <= 0 {
			return Config{}, fmt.Errorf("max_refresh_concurrency must be positive")
		}
		cfg.MaxRefreshConcurrency = *decoded.MaxRefreshConcurrency
	}
	if decoded.QuotaEndpoint != "" {
		endpoint, err := validateQuotaEndpoint(decoded.QuotaEndpoint)
		if err != nil {
			return Config{}, err
		}
		cfg.QuotaEndpoint = endpoint
	}
	if decoded.RefreshActiveWindow != "" {
		d, err := time.ParseDuration(decoded.RefreshActiveWindow)
		if err != nil {
			return Config{}, fmt.Errorf("refresh_active_window: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("refresh_active_window must be positive")
		}
		cfg.RefreshActiveWindow = d
	}
	if decoded.RefreshAfterResetDelay != "" {
		d, err := time.ParseDuration(decoded.RefreshAfterResetDelay)
		if err != nil {
			return Config{}, fmt.Errorf("refresh_after_reset_delay: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("refresh_after_reset_delay must be positive")
		}
		cfg.RefreshAfterResetDelay = d
	}
	if decoded.RefreshRetryDelays != "" {
		delays, err := parseDurationList(decoded.RefreshRetryDelays)
		if err != nil {
			return Config{}, fmt.Errorf("refresh_retry_delays: %w", err)
		}
		cfg.RefreshRetryDelays = delays
	}
	if decoded.RefreshOnStartup != nil {
		cfg.RefreshOnStartup = *decoded.RefreshOnStartup
	}
	if decoded.CircuitFailureThreshold != nil {
		if *decoded.CircuitFailureThreshold <= 0 {
			return Config{}, fmt.Errorf("circuit_failure_threshold must be positive")
		}
		cfg.CircuitFailureThreshold = *decoded.CircuitFailureThreshold
	}
	if decoded.CircuitOpenDuration != "" {
		d, err := time.ParseDuration(decoded.CircuitOpenDuration)
		if err != nil {
			return Config{}, fmt.Errorf("circuit_open_duration: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("circuit_open_duration must be positive")
		}
		cfg.CircuitOpenDuration = d
	}
	if decoded.CircuitHalfOpenSuccessThreshold != nil {
		if *decoded.CircuitHalfOpenSuccessThreshold <= 0 {
			return Config{}, fmt.Errorf("circuit_half_open_success_threshold must be positive")
		}
		cfg.CircuitHalfOpenSuccessThreshold = *decoded.CircuitHalfOpenSuccessThreshold
	}
	if decoded.MaxLogEntries != nil {
		if *decoded.MaxLogEntries <= 0 {
			return Config{}, fmt.Errorf("max_log_entries must be positive")
		}
		cfg.MaxLogEntries = *decoded.MaxLogEntries
	}
	if decoded.LogRetention != "" {
		d, err := time.ParseDuration(decoded.LogRetention)
		if err != nil {
			return Config{}, fmt.Errorf("log_retention: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("log_retention must be positive")
		}
		cfg.LogRetention = d
	}
	return NormalizeConfig(cfg), nil
}

func normalizeRetryDelays(values, fallback []time.Duration) []time.Duration {
	normalized := make([]time.Duration, 0, len(values))
	for _, value := range values {
		if value > 0 {
			normalized = append(normalized, value)
		}
	}
	if len(normalized) == 0 {
		return append([]time.Duration(nil), fallback...)
	}
	return normalized
}

func parseDurationList(raw string) ([]time.Duration, error) {
	parts := strings.Split(raw, ",")
	delays := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		d, err := time.ParseDuration(part)
		if err != nil {
			return nil, err
		}
		if d <= 0 {
			return nil, fmt.Errorf("duration must be positive")
		}
		delays = append(delays, d)
	}
	if len(delays) == 0 {
		return nil, fmt.Errorf("at least one duration is required")
	}
	return delays, nil
}

func validateResetProbeModel(raw string) (string, error) {
	model := strings.TrimSpace(raw)
	if model == "" {
		return "", fmt.Errorf("reset_probe_model must not be empty")
	}
	if strings.ContainsAny(model, " \t\r\n") {
		return "", fmt.Errorf("reset_probe_model must not contain whitespace")
	}
	return model, nil
}

func validateQuotaEndpoint(raw string) (string, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		return "", nil
	}
	if endpoint != chatGPTQuotaEndpoint {
		return "", fmt.Errorf("quota_endpoint must be %s", chatGPTQuotaEndpoint)
	}
	return endpoint, nil
}

func PluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             PluginID,
			Version:          pluginVersion,
			Author:           "HiJean99",
			GitHubRepository: "https://github.com/HiJean99/cpa-plugin-codex-fleet-manager",
			Logo:             "https://raw.githubusercontent.com/router-for-me/CLIProxyAPI/main/docs/logo.png",
		},
		Capabilities: registrationCapabilities{
			Scheduler:     true,
			UsagePlugin:   true,
			ManagementAPI: true,
		},
	}
}
