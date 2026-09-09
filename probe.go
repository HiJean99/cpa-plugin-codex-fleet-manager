package main

import (
	"encoding/json"
	"time"
)

const (
	resetProbeAfterResetDelay = 10 * time.Minute // legacy state compatibility only
	resetProbeCloseThreshold  = 3 * time.Minute
	codexResetProbeEndpoint   = "https://chatgpt.com/backend-api/codex/responses"
)

func probeWindowDuration(window QuotaWindow) (time.Duration, bool) {
	if window.LimitWindowSeconds != nil && *window.LimitWindowSeconds > 0 {
		return time.Duration(*window.LimitWindowSeconds) * time.Second, true
	}
	switch window.Kind {
	case WindowFiveHour:
		return 5 * time.Hour, true
	case WindowWeekly:
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func firstObservationLazyWindow(now time.Time, window QuotaWindow) (time.Duration, bool) {
	if window.UsedPercent == nil || *window.UsedPercent != 0 || window.ResetAt.IsZero() {
		return 0, false
	}
	duration, ok := probeWindowDuration(window)
	if !ok || duration <= 0 {
		return 0, false
	}
	return duration, absDuration(window.ResetAt.Sub(now.Add(duration))) <= resetProbeCloseThreshold
}

func scheduleResetProbesFromPrevious(previous AccountState, current map[WindowKind]ResetProbeState, now time.Time) map[WindowKind]ResetProbeState {
	next := cloneResetProbes(current)
	for _, window := range resetProbeCandidateWindows(previous.Quota) {
		duration, ok := probeWindowDuration(window)
		if !ok || duration <= 0 || window.ResetAt.IsZero() || window.ResetAt.After(now) {
			continue
		}
		if existing, ok := next[window.Kind]; ok && existing.ResetAt.Equal(window.ResetAt) {
			continue
		}
		if next == nil {
			next = make(map[WindowKind]ResetProbeState)
		}
		next[window.Kind] = ResetProbeState{
			WindowKind:    window.Kind,
			WindowSeconds: int64(duration / time.Second),
			ResetAt:       window.ResetAt,
			NextCheckAt:   window.ResetAt.Add(resetProbeAfterResetDelay),
			Status:        ResetProbeStatusPending,
		}
	}
	return next
}

func resetProbeCandidateWindows(quota ParsedQuota) []QuotaWindow {
	windows := make([]QuotaWindow, 0, 2)
	if quota.FiveHour != nil {
		windows = append(windows, *quota.FiveHour)
	}
	if quota.LongWindow != nil {
		windows = append(windows, *quota.LongWindow)
	}
	return windows
}

func resetProbeDue(probe ResetProbeState, now time.Time) bool {
	return probe.Status == ResetProbeStatusPending && !probe.NextCheckAt.IsZero() && !probe.NextCheckAt.After(now)
}

func resetProbeDueAny(probes map[WindowKind]ResetProbeState, now time.Time) bool {
	for _, probe := range probes {
		if resetProbeDue(probe, now) {
			return true
		}
	}
	return false
}

func matchingProbeWindow(quota ParsedQuota, probe ResetProbeState) (QuotaWindow, bool) {
	for _, window := range resetProbeCandidateWindows(quota) {
		if probe.WindowKind != "" && window.Kind != probe.WindowKind {
			continue
		}
		return window, true
	}
	return QuotaWindow{}, false
}

func markResetProbeConfirmedActive(probe ResetProbeState) ResetProbeState {
	probe.Status = ResetProbeStatusConfirmedActive
	probe.Error = ""
	return probe
}

func markResetProbeVerified(probe ResetProbeState, now time.Time) ResetProbeState {
	probe.Status = ResetProbeStatusVerified
	probe.LastProbeAt = now
	probe.VerifiedAt = now
	probe.Error = ""
	return probe
}

func markResetProbeRetry(probe ResetProbeState, now time.Time, err error, credentials CodexCredentials, cfg Config) ResetProbeState {
	probe.LastProbeAt = now
	probe.Attempts++
	probe.Error = ""
	if err != nil {
		probe.Error = redactWithCredentials(err.Error(), credentials)
	}
	retryDelays := NormalizeConfig(cfg).RefreshRetryDelays
	if len(retryDelays) == 0 || probe.Attempts > len(retryDelays) {
		probe.Status = ResetProbeStatusFailed
		probe.NextCheckAt = time.Time{}
		return probe
	}
	probe.Status = ResetProbeStatusPending
	probe.NextCheckAt = now.Add(retryDelays[probe.Attempts-1])
	return probe
}

func looksLikeLazyReset(now time.Time, window QuotaWindow, windowSeconds int64) bool {
	if windowSeconds <= 0 || window.ResetAt.IsZero() {
		return false
	}
	duration := time.Duration(windowSeconds) * time.Second
	return absDuration(window.ResetAt.Sub(now.Add(duration))) <= resetProbeCloseThreshold
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func resetProbeUsageEvidence(body []byte) bool {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return false
	}
	for _, path := range [][]string{
		{"usage", "total_tokens"},
		{"usage", "prompt_tokens"},
		{"usage", "input_tokens"},
		{"usage", "completion_tokens"},
		{"usage", "output_tokens"},
		{"response", "usage", "total_tokens"},
		{"response", "usage", "prompt_tokens"},
		{"response", "usage", "input_tokens"},
		{"response", "usage", "completion_tokens"},
		{"response", "usage", "output_tokens"},
	} {
		if jsonNumberPathPositive(doc, path...) {
			return true
		}
	}
	return false
}

func jsonNumberPathPositive(root any, path ...string) bool {
	current := root
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = obj[key]
		if !ok {
			return false
		}
	}
	value, ok := current.(float64)
	return ok && value > 0
}

func resetProbePayloadBytes(cfg Config) []byte {
	cfg = NormalizeConfig(cfg)
	payload := struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		Input        []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
		Stream bool `json:"stream"`
		Store  bool `json:"store"`
	}{Model: cfg.ResetProbeModel, Instructions: "Reply with OK.", Stream: true, Store: false}
	message := struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}{Type: "message", Role: "user"}
	message.Content = append(message.Content, struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{Type: "input_text", Text: "hi"})
	payload.Input = append(payload.Input, message)
	raw, _ := json.Marshal(payload)
	return raw
}
