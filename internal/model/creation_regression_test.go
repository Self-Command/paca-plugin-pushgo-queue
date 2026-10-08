package model

import (
	"testing"
	"time"
)

// A series is announced once; materializing its next thirty dates is not thirty
// independent user creations. Dates still have their ordinary timed reminders.
func TestSeriesCreationDoesNotAnnounceEveryOccurrence(t *testing.T) {
	now := time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC)
	c := DefaultConfig()
	c.CreatedPush = true
	mother := Task{ID: "mother", Title: "每日任务", CreatedAt: now, Custom: map[string]any{"_integration_state_v1": map[string]any{"recurring": true}}}
	specs, _ := Compute(mother, c, nil, false)
	if len(specs) != 1 || specs[0].Kind != "created" {
		t.Fatal("a newly created series must have exactly one creation announcement and no timed mother reminders")
	}
	for day := 0; day < 31; day++ {
		target := now.Add(time.Duration(day+1) * 24 * time.Hour)
		core := time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, time.UTC)
		period := Task{CreatedAt: now, Start: &core, Custom: map[string]any{"_integration_state_v1": map[string]any{"series_parent": "mother", "occurrence_date": target.Format("2006-01-02"), "start_precision": "instant", "start_instant": target.Format(time.RFC3339), "start_core_date": core.Format("2006-01-02"), "timezone": "UTC"}}}
		got, _ := Compute(period, c, nil, false)
		if len(got) != 1 || got[0].Kind != "start" {
			t.Fatalf("period %d must keep its start reminder without a creation announcement: %+v", day, got)
		}
	}
}
