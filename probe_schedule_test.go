package main

import (
	"reflect"
	"testing"
	"time"
)

func TestParseResetProbeScheduleNormalizesAndSorts(t *testing.T) {
	got, err := parseResetProbeSchedule("17:00, 07:00,12:00,07:00")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"07:00", "12:00", "17:00"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schedule = %#v, want %#v", got, want)
	}
}

func TestScheduledProbeDeadlineCatchesCurrentSlotWithinGrace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ResetProbeMode = ResetProbeModeSchedule
	cfg.ResetProbeTimezone = "Asia/Shanghai"
	cfg.ResetProbeSchedule = []string{"07:00", "12:00", "17:00"}
	cfg.ResetProbeScheduleGrace = 15 * time.Minute
	now := time.Date(2026, 9, 9, 7, 5, 0, 0, time.FixedZone("CST", 8*3600))
	got := scheduledProbeDeadline(cfg, now, time.Time{})
	want := time.Date(2026, 9, 9, 7, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if !got.Equal(want) {
		t.Fatalf("deadline = %s, want %s", got, want)
	}
	got = scheduledProbeDeadline(cfg, now, got)
	want = time.Date(2026, 9, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if !got.Equal(want) {
		t.Fatalf("next deadline = %s, want %s", got, want)
	}
}

func TestScheduledProbeDeadlineSkipsMissedSlotAfterGrace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ResetProbeMode = ResetProbeModeSchedule
	cfg.ResetProbeTimezone = "Asia/Shanghai"
	cfg.ResetProbeSchedule = []string{"07:00", "12:00", "17:00"}
	cfg.ResetProbeScheduleGrace = 15 * time.Minute
	now := time.Date(2026, 9, 9, 7, 20, 0, 0, time.FixedZone("CST", 8*3600))
	got := scheduledProbeDeadline(cfg, now, time.Time{})
	want := time.Date(2026, 9, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if !got.Equal(want) {
		t.Fatalf("deadline = %s, want %s", got, want)
	}
}

func TestNextScheduledProbeAfterRollsToNextDay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ResetProbeMode = ResetProbeModeSchedule
	cfg.ResetProbeTimezone = "Asia/Shanghai"
	cfg.ResetProbeSchedule = []string{"07:00", "12:00", "17:00"}
	now := time.Date(2026, 9, 9, 22, 0, 0, 0, time.FixedZone("CST", 8*3600))
	got := nextScheduledProbeAfter(cfg, now)
	want := time.Date(2026, 9, 10, 7, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if !got.Equal(want) {
		t.Fatalf("next = %s, want %s", got, want)
	}
}

func TestScheduledFiveHourPrecheckSendsForExpiredUnchangedWindow(t *testing.T) {
	now := time.Date(2026, 9, 9, 7, 0, 0, 0, time.FixedZone("CST", 8*3600))
	next := now.Add(5 * time.Hour)
	zero := 0.0
	reset := now.Add(-9 * time.Hour)
	base := ResetProbeBaseline(reset, 0, 5*time.Hour)
	base.WindowKind = WindowFiveHour
	base.SuspectedLazy = true
	controller := NewProbeController(now)
	controller.SetWindow(1, ProbeWindowFiveHour, ProbeWindow{State: ProbePendingCheck, Baseline: base, LastScheduleSlot: now})

	intents := controller.Advance(1, ProbeEvent{
		Kind:                  ProbeEventPrecheckResult,
		Now:                   now,
		ScheduledFiveHourNext: next,
		Snapshots: map[ProbeWindowKind]QuotaSnapshot{
			ProbeWindowFiveHour: {Valid: true, ResetAt: &reset, Usage: &zero, WindowKind: WindowFiveHour, WindowLength: 5 * time.Hour, WindowLengthKnown: true},
		},
	})
	if len(intents) != 1 || intents[0].Class != OperationProbeSend {
		t.Fatalf("intents = %#v, want one scheduled activation send", intents)
	}
	window, _ := controller.Window(1, ProbeWindowFiveHour)
	if window.State != ProbeSentAwaitingVerify {
		t.Fatalf("state = %s, want %s", window.State, ProbeSentAwaitingVerify)
	}

	one := 1.0
	activeReset := now.Add(5 * time.Hour)
	controller.Advance(1, ProbeEvent{
		Kind:                  ProbeEventVerifyResult,
		Now:                   now.Add(4 * time.Second),
		ScheduledFiveHourNext: next,
		Snapshots: map[ProbeWindowKind]QuotaSnapshot{
			ProbeWindowFiveHour: {Valid: true, ResetAt: &activeReset, Usage: &one, WindowKind: WindowFiveHour, WindowLength: 5 * time.Hour, WindowLengthKnown: true},
		},
	})
	window, _ = controller.Window(1, ProbeWindowFiveHour)
	if window.State != ProbeWaitingReset || !window.Deadline.Equal(next) {
		t.Fatalf("verified window = %#v, want next scheduled deadline %s", window, next)
	}
}

func TestScheduledFiveHourPrecheckSkipsAlreadyActiveWindow(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	next := now.Add(5 * time.Hour)
	oldReset := now.Add(-5 * time.Hour)
	base := ResetProbeBaseline(oldReset, 0, 5*time.Hour)
	base.WindowKind = WindowFiveHour
	base.SuspectedLazy = true
	controller := NewProbeController(now)
	controller.SetWindow(1, ProbeWindowFiveHour, ProbeWindow{State: ProbePendingCheck, Baseline: base, LastScheduleSlot: now})

	one := 1.0
	activeReset := now.Add(4*time.Hour + 50*time.Minute)
	intents := controller.Advance(1, ProbeEvent{
		Kind:                  ProbeEventPrecheckResult,
		Now:                   now,
		ScheduledFiveHourNext: next,
		Snapshots: map[ProbeWindowKind]QuotaSnapshot{
			ProbeWindowFiveHour: {Valid: true, ResetAt: &activeReset, Usage: &one, WindowKind: WindowFiveHour, WindowLength: 5 * time.Hour, WindowLengthKnown: true},
		},
	})
	if len(intents) != 0 {
		t.Fatalf("intents = %#v, want no activation for an already active 5h window", intents)
	}
	window, _ := controller.Window(1, ProbeWindowFiveHour)
	if window.State != ProbeWaitingReset || !window.Deadline.Equal(next) {
		t.Fatalf("window = %#v, want next scheduled deadline %s", window, next)
	}
}

func TestBootstrapScheduledFiveHourUsesWallClockDeadline(t *testing.T) {
	now := time.Date(2026, 9, 9, 6, 30, 0, 0, time.FixedZone("CST", 8*3600))
	cfg := DefaultConfig()
	cfg.EnableResetProbe = true
	cfg.ResetProbeMode = ResetProbeModeSchedule
	cfg.ResetProbeTimezone = "Asia/Shanghai"
	cfg.ResetProbeSchedule = []string{"07:00", "12:00", "17:00"}
	state := NewPluginState(cfg)
	priority := 0
	r := &QuotaRefresher{state: state, now: func() time.Time { return now }, probeController: NewProbeController(now), roster: HostRosterSnapshot{Capability: CapabilityA, Entries: []RosterEntry{{ID: "a", AuthIndex: "idx-a", Provider: "codex", Priority: &priority}}}}
	touched := map[AuthInstanceID]struct{}{}
	r.bootstrapScheduledFiveHourWindowsLocked(map[string]RuntimeBinding{"a": {AuthID: "a", Instance: 1}}, now, touched)
	window, ok := r.probeController.Window(1, ProbeWindowFiveHour)
	if !ok {
		t.Fatal("scheduled five-hour window missing")
	}
	want := time.Date(2026, 9, 9, 7, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if window.State != ProbeWaitingReset || !window.Deadline.Equal(want) {
		t.Fatalf("window = %#v, want waiting deadline %s", window, want)
	}
	if window.Baseline.Kind != ProbeBaselineNone {
		t.Fatalf("baseline = %#v, want quota-independent empty baseline", window.Baseline)
	}
	if _, ok := touched[1]; !ok {
		t.Fatal("scheduled bootstrap did not mark instance touched")
	}
}

func TestScheduledFiveHourWaitsForNearResetWithinGrace(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	next := time.Date(2026, 9, 9, 17, 0, 0, 0, now.Location())
	graceEnd := now.Add(15 * time.Minute)
	one := 1.0
	oldReset := now.Add(-5 * time.Hour)
	base := ResetProbeBaseline(oldReset, 0, 5*time.Hour)
	base.WindowKind = WindowFiveHour
	base.SuspectedLazy = true
	controller := NewProbeController(now)
	controller.SetWindow(1, ProbeWindowFiveHour, ProbeWindow{State: ProbePendingCheck, Baseline: base, LastScheduleSlot: now})

	activeReset := now.Add(4 * time.Second)
	intents := controller.Advance(1, ProbeEvent{
		Kind:                      ProbeEventPrecheckResult,
		Now:                       now,
		ScheduledFiveHourNext:     next,
		ScheduledFiveHourGraceEnd: graceEnd,
		Snapshots: map[ProbeWindowKind]QuotaSnapshot{
			ProbeWindowFiveHour: {Valid: true, ResetAt: &activeReset, Usage: &one, WindowKind: WindowFiveHour, WindowLength: 5 * time.Hour, WindowLengthKnown: true},
		},
	})
	if len(intents) != 0 {
		t.Fatalf("intents = %#v, want no send before the old window matures", intents)
	}
	window, _ := controller.Window(1, ProbeWindowFiveHour)
	want := activeReset.Add(probeRefreshAfterResetDelay)
	if window.State != ProbeWaitingReset || !window.Deadline.Equal(want) {
		t.Fatalf("window = %#v, want catch-up deadline %s", window, want)
	}
}

func TestScheduleModeDisablesLongWindowActivation(t *testing.T) {
	now := time.Date(2026, 9, 9, 6, 30, 0, 0, time.FixedZone("CST", 8*3600))
	cfg := DefaultConfig()
	cfg.EnableResetProbe = true
	cfg.ResetProbeMode = ResetProbeModeSchedule
	state := NewPluginState(cfg)
	r := &QuotaRefresher{state: state, now: func() time.Time { return now }, probeController: NewProbeController(now), roster: HostRosterSnapshot{Capability: CapabilityA}}
	longBase := ResetProbeBaseline(now.Add(time.Hour), 0, 7*24*time.Hour)
	longBase.WindowKind = WindowWeekly
	r.probeController.SetWindow(1, ProbeWindowLong, ProbeWindow{State: ProbeWaitingReset, Baseline: longBase, Deadline: now.Add(time.Hour)})
	touched := map[AuthInstanceID]struct{}{}
	r.bootstrapScheduledFiveHourWindowsLocked(nil, now, touched)
	window, ok := r.probeController.Window(1, ProbeWindowLong)
	if !ok || window.State != ProbeIdle || !window.Deadline.IsZero() {
		t.Fatalf("long window = %#v, want idle with no deadline", window)
	}
}

func TestScheduledFiveHourPrecheckWithoutQuotaBaselineSendsWhenExpired(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	next := now.Add(5 * time.Hour)
	zero := 0.0
	expired := now.Add(-time.Minute)
	controller := NewProbeController(now)
	controller.SetWindow(1, ProbeWindowFiveHour, ProbeWindow{
		State:            ProbePendingCheck,
		Baseline:         ProbeBaseline{Kind: ProbeBaselineNone, WindowKind: WindowFiveHour},
		LastScheduleSlot: now,
	})
	intents := controller.Advance(1, ProbeEvent{
		Kind:                  ProbeEventPrecheckResult,
		Now:                   now,
		ScheduledFiveHourNext: next,
		Snapshots: map[ProbeWindowKind]QuotaSnapshot{
			ProbeWindowFiveHour: {Valid: true, ResetAt: &expired, Usage: &zero, WindowKind: WindowFiveHour, WindowLength: 5 * time.Hour, WindowLengthKnown: true},
		},
	})
	if len(intents) != 1 || intents[0].Class != OperationProbeSend {
		t.Fatalf("intents = %#v, want one activation send from quota-independent schedule baseline", intents)
	}
	window, ok := controller.Window(1, ProbeWindowFiveHour)
	if !ok || window.State != ProbeSentAwaitingVerify {
		t.Fatalf("window = %#v, want sent-awaiting-verify", window)
	}
}

func TestScheduledFiveHourInitialBaselineWaitsForNearResetWithinGrace(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	next := now.Add(5 * time.Hour)
	graceEnd := now.Add(15 * time.Minute)
	zero := 0.0
	nearReset := now.Add(4 * time.Second)
	controller := NewProbeController(now)
	controller.SetWindow(1, ProbeWindowFiveHour, ProbeWindow{
		State:            ProbePendingCheck,
		Baseline:         ProbeBaseline{Kind: ProbeBaselineNone, WindowKind: WindowFiveHour},
		LastScheduleSlot: now,
	})
	intents := controller.Advance(1, ProbeEvent{
		Kind:                      ProbeEventPrecheckResult,
		Now:                       now,
		ScheduledFiveHourNext:     next,
		ScheduledFiveHourGraceEnd: graceEnd,
		Snapshots: map[ProbeWindowKind]QuotaSnapshot{
			ProbeWindowFiveHour: {Valid: true, ResetAt: &nearReset, Usage: &zero, WindowKind: WindowFiveHour, WindowLength: 5 * time.Hour, WindowLengthKnown: true},
		},
	})
	if len(intents) != 0 {
		t.Fatalf("intents = %#v, want no activation before near reset matures", intents)
	}
	window, ok := controller.Window(1, ProbeWindowFiveHour)
	if !ok {
		t.Fatal("scheduled window missing")
	}
	want := nearReset.Add(probeRefreshAfterResetDelay)
	if window.State != ProbeWaitingReset || !window.Deadline.Equal(want) {
		t.Fatalf("window = %#v, want catch-up deadline %s", window, want)
	}
}
