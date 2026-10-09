package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUnifiedTimePatchPreservesFieldsAndPrecision(t *testing.T) {
	base := Task{Custom: map[string]any{"business": map[string]any{"retained": true}, "_checkin_state_v1": map[string]any{"record": "keep"}, "_integration_state_v1": map[string]any{"recurring": true}}}
	input := TaskTimes{Start: TimeValue{"instant", "2026-10-09T09:00"}, Due: TimeValue{"day", "2026-10-10"}, Timezone: "Asia/Shanghai", StartMinutes: 10, DueMinutes: 10}
	patch, err := TimePatch(base, input)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(patch)
	var actual Task
	if json.Unmarshal(raw, &actual) != nil {
		t.Fatal("patch not a core task shape")
	}
	if actual.Custom["business"] == nil || actual.Custom["_checkin_state_v1"] == nil || Meta(actual)["recurring"] != true {
		t.Fatal("unmapped fields were lost")
	}
	if Meta(base)["start_precision"] != nil {
		t.Fatal("patch mutated its base snapshot")
	}
	got := TimesOf(actual)
	if got.Start.Precision != "instant" || got.Start.Value != "2026-10-09T01:00:00Z" || got.Due.Precision != "day" {
		t.Fatal(got)
	}
	newDate := actual.Start.Add(24 * time.Hour)
	actual.Start = &newDate
	if TimesOf(actual).Start.Precision != "day" {
		t.Fatal("official date edit inherited an unconfirmed old wall time")
	}
}

func TestUnifiedTimesRejectInvertedAndNonexistentTimes(t *testing.T) {
	for _, input := range []TaskTimes{
		{Start: TimeValue{"instant", "2026-10-09T10:00"}, Due: TimeValue{"instant", "2026-10-09T09:00"}, Timezone: "Asia/Shanghai"},
		{Start: TimeValue{"day", "2026-02-30"}, Due: TimeValue{"none", ""}, Timezone: "Asia/Shanghai"},
		{Start: TimeValue{"instant", "2026-03-08T02:30"}, Due: TimeValue{"none", ""}, Timezone: "America/New_York"},
	} {
		if _, err := TimePatch(Task{}, input); err == nil {
			t.Fatal("invalid time accepted", input)
		}
	}
}

func TestZeroLeadOnlyAllowedWithoutCheckin(t *testing.T) {
	value := TaskTimes{Start: TimeValue{Precision: "instant"}, Due: TimeValue{Precision: "instant"}, StartMinutes: 0, DueMinutes: 10}
	if value.ValidateCheckinLead(true) == nil {
		t.Fatal("empty start window accepted")
	}
	if value.ValidateCheckinLead(false) != nil {
		t.Fatal("ordinary zero-lead reminder removed")
	}
	value.StartMinutes = 10
	value.DueMinutes = 0
	if value.ValidateCheckinLead(true) == nil {
		t.Fatal("empty due window accepted")
	}
	value.DueMinutes = 1
	if value.ValidateCheckinLead(true) != nil {
		t.Fatal("valid check-in lead rejected")
	}
	value.Start = TimeValue{Precision: "day"}
	value.Due = TimeValue{Precision: "none"}
	value.StartMinutes = 0
	value.DueMinutes = 0
	if value.ValidateCheckinLead(true) != nil {
		t.Fatal("date-only task does not have a check-in window")
	}
}
