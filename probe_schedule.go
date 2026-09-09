package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type ResetProbeMode string

const (
	ResetProbeModeResetAt  ResetProbeMode = "reset_at"
	ResetProbeModeSchedule ResetProbeMode = "schedule"
)

type probeScheduleClock struct {
	Hour   int
	Minute int
	Text   string
}

func validateResetProbeMode(raw string) (ResetProbeMode, error) {
	mode := ResetProbeMode(strings.TrimSpace(raw))
	switch mode {
	case ResetProbeModeResetAt, ResetProbeModeSchedule:
		return mode, nil
	default:
		return "", fmt.Errorf("reset_probe_mode must be %q or %q", ResetProbeModeResetAt, ResetProbeModeSchedule)
	}
}

func validateResetProbeTimezone(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("reset_probe_timezone must not be empty")
	}
	if _, err := time.LoadLocation(name); err != nil {
		return "", fmt.Errorf("reset_probe_timezone: %w", err)
	}
	return name, nil
}

func parseResetProbeSchedule(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	entries := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		entries = append(entries, part)
	}
	return validateResetProbeSchedule(entries)
}

func validateResetProbeSchedule(entries []string) ([]string, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("reset_probe_schedule must contain at least one HH:MM entry")
	}
	clocks := make([]probeScheduleClock, 0, len(entries))
	seen := map[int]struct{}{}
	for _, entry := range entries {
		clock, err := parseProbeScheduleClock(entry)
		if err != nil {
			return nil, err
		}
		key := clock.Hour*60 + clock.Minute
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		clocks = append(clocks, clock)
	}
	sort.Slice(clocks, func(i, j int) bool {
		return clocks[i].Hour*60+clocks[i].Minute < clocks[j].Hour*60+clocks[j].Minute
	})
	out := make([]string, 0, len(clocks))
	for _, clock := range clocks {
		out = append(out, clock.Text)
	}
	return out, nil
}

func parseProbeScheduleClock(raw string) (probeScheduleClock, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return probeScheduleClock{}, fmt.Errorf("reset_probe_schedule entry %q must use HH:MM", raw)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return probeScheduleClock{}, fmt.Errorf("reset_probe_schedule entry %q has invalid hour", raw)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return probeScheduleClock{}, fmt.Errorf("reset_probe_schedule entry %q has invalid minute", raw)
	}
	return probeScheduleClock{Hour: hour, Minute: minute, Text: fmt.Sprintf("%02d:%02d", hour, minute)}, nil
}

func resetProbeScheduleClocks(cfg Config) (*time.Location, []probeScheduleClock, error) {
	cfg = NormalizeConfig(cfg)
	loc, err := time.LoadLocation(cfg.ResetProbeTimezone)
	if err != nil {
		return nil, nil, err
	}
	entries, err := validateResetProbeSchedule(cfg.ResetProbeSchedule)
	if err != nil {
		return nil, nil, err
	}
	clocks := make([]probeScheduleClock, 0, len(entries))
	for _, entry := range entries {
		clock, err := parseProbeScheduleClock(entry)
		if err != nil {
			return nil, nil, err
		}
		clocks = append(clocks, clock)
	}
	return loc, clocks, nil
}

func scheduledProbeCurrentSlot(cfg Config, now time.Time) (time.Time, bool) {
	cfg = NormalizeConfig(cfg)
	if cfg.ResetProbeMode != ResetProbeModeSchedule {
		return time.Time{}, false
	}
	loc, clocks, err := resetProbeScheduleClocks(cfg)
	if err != nil {
		return time.Time{}, false
	}
	localNow := now.In(loc)
	for _, clock := range clocks {
		slot := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), clock.Hour, clock.Minute, 0, 0, loc)
		if !localNow.Before(slot) && localNow.Before(slot.Add(cfg.ResetProbeScheduleGrace)) {
			return slot, true
		}
	}
	return time.Time{}, false
}

func nextScheduledProbeAfter(cfg Config, now time.Time) time.Time {
	cfg = NormalizeConfig(cfg)
	if cfg.ResetProbeMode != ResetProbeModeSchedule {
		return time.Time{}
	}
	loc, clocks, err := resetProbeScheduleClocks(cfg)
	if err != nil || len(clocks) == 0 {
		return time.Time{}
	}
	localNow := now.In(loc)
	for dayOffset := 0; dayOffset <= 1; dayOffset++ {
		day := localNow.AddDate(0, 0, dayOffset)
		for _, clock := range clocks {
			slot := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour, clock.Minute, 0, 0, loc)
			if slot.After(localNow) {
				return slot
			}
		}
	}
	return time.Time{}
}

func scheduledProbeDeadline(cfg Config, now, lastSlot time.Time) time.Time {
	if slot, ok := scheduledProbeCurrentSlot(cfg, now); ok {
		if lastSlot.IsZero() || !lastSlot.Equal(slot) {
			return slot
		}
	}
	return nextScheduledProbeAfter(cfg, now)
}
