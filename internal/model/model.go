// Package model defines time precision and reminder identity independently of TaskNotes.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	_ "time/tzdata"
)

type Task struct {
	ID          string         `json:"id"`
	TaskNumber  int64          `json:"task_number"`
	ProjectID   string         `json:"project_id"`
	Title       string         `json:"title"`
	StatusID    string         `json:"status_id"`
	Importance  int            `json:"importance"`
	Start       *time.Time     `json:"start_date"`
	Due         *time.Time     `json:"due_date"`
	CreatedAt   time.Time      `json:"created_at"`
	Custom      map[string]any `json:"custom_fields"`
	Description any            `json:"description"`
	Tags        []string       `json:"tags"`
}
type Config struct {
	Enabled        bool              `json:"enabled"`
	GatewayURL     string            `json:"gateway_url"`
	ChannelID      string            `json:"channel_id"`
	ChannelName    string            `json:"channel_name"`
	Timezone       string            `json:"timezone"`
	StartMinutes   int               `json:"start_minutes"`
	DueMinutes     int               `json:"due_minutes"`
	CreatedPush    bool              `json:"created_push"`
	CheckinEnabled bool              `json:"checkin_enabled"`
	PriorityMap    map[string]string `json:"priority_map"`
}
type Rule struct {
	Enabled         bool       `json:"enabled"`
	Timezone        string     `json:"timezone"`
	StartEnabled    bool       `json:"start_enabled"`
	DueEnabled      bool       `json:"due_enabled"`
	Start           *time.Time `json:"start"`
	Due             *time.Time `json:"due"`
	StartMinutes    *int       `json:"start_minutes"`
	DueMinutes      *int       `json:"due_minutes"`
	BaseFingerprint string     `json:"base_fingerprint"`
}
type Spec struct {
	Kind     string    `json:"kind"`
	Target   time.Time `json:"target"`
	Fire     time.Time `json:"fire"`
	Expires  time.Time `json:"expires"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	Severity string    `json:"severity"`
	Binding  string    `json:"binding"`
}

func Hash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func Meta(t Task) map[string]any {
	m, _ := t.Custom["_integration_state_v1"].(map[string]any)
	return m
}
func Fingerprint(t Task) string {
	m := Meta(t)
	return Hash(map[string]any{"start": instant(t.Start), "due": instant(t.Due), "start_source": m["start_source"], "due_source": m["due_source"], "start_precision": m["start_precision"], "due_precision": m["due_precision"], "start_instant": m["start_instant"], "due_instant": m["due_instant"], "start_core_date": m["start_core_date"], "due_core_date": m["due_core_date"], "timezone": m["timezone"]})
}
func instant(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func precision(t Task, kind string, core *time.Time) *time.Time {
	m := Meta(t)
	if m[kind+"_precision"] != "instant" || core == nil {
		return nil
	}
	v, _ := m[kind+"_instant"].(string)
	parsed, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return nil
	}
	day, _ := m[kind+"_core_date"].(string)
	zone, _ := m["timezone"].(string)
	loc, err := time.LoadLocation(zone)
	if err != nil || day == "" || day != core.UTC().Format("2006-01-02") || day != parsed.In(loc).Format("2006-01-02") {
		return nil
	}
	target := parsed.UTC()
	return &target
}
func DefaultConfig() Config {
	return Config{Enabled: true, Timezone: "Asia/Shanghai", StartMinutes: 10, DueMinutes: 10, PriorityMap: map[string]string{"none": "normal", "low": "low", "medium": "normal", "high": "high", "critical": "critical"}}
}
func ValidConfig(c Config) bool {
	u, err := url.Parse(c.GatewayURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if c.ChannelID == "" || len(c.ChannelID) > 64 || c.StartMinutes < 0 || c.StartMinutes > 1440 || c.DueMinutes < 0 || c.DueMinutes > 1440 {
		return false
	}
	if _, err = time.LoadLocation(c.Timezone); err != nil || c.Timezone == "" {
		return false
	}
	for _, v := range c.PriorityMap {
		if v != "low" && v != "normal" && v != "high" && v != "critical" {
			return false
		}
	}
	return true
}
func Severity(importance int, mapping map[string]string) string {
	bucket := "none"
	switch {
	case importance >= 100:
		bucket = "critical"
	case importance >= 50:
		bucket = "high"
	case importance >= 20:
		bucket = "medium"
	case importance > 0:
		bucket = "low"
	}
	value := mapping[bucket]
	if value == "" {
		value = "normal"
	}
	return value
}
func ParseTime(s, zone string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		utc := t.UTC()
		return &utc, nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, err
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04"} {
		wall, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		t, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			continue
		}
		normalized := wall.Format("2006-01-02T15:04:05.999999999")
		if t.In(loc).Format("2006-01-02T15:04:05.999999999") != normalized {
			continue
		}
		for _, delta := range []time.Duration{-2 * time.Hour, -time.Hour, time.Hour, 2 * time.Hour} {
			if t.Add(delta).In(loc).Format("2006-01-02T15:04:05.999999999") == normalized {
				return nil, errors.New("ambiguous local time; include UTC offset")
			}
		}
		utc := t.UTC()
		return &utc, nil
	}
	return nil, errors.New("precise time required; invalid or nonexistent local time")
}
func Compute(t Task, c Config, r *Rule, done bool) ([]Spec, string) {
	m := Meta(t)
 if zone,ok:=m["timezone"].(string);ok&&zone!=""{c.Timezone=zone}
	if !c.Enabled || done || m["archived"] == true || (r != nil && !r.Enabled) {
		return []Spec{}, "cancelled"
	}
	start, due := precision(t, "start", t.Start), precision(t, "due", t.Due)
	sm, dm := c.StartMinutes, c.DueMinutes
	if v,ok:=m["reminder_start_minutes"].(float64);ok {sm=int(v)}
	if v,ok:=m["reminder_due_minutes"].(float64);ok {dm=int(v)}
	se, de := true, true
	stale := false
	if r != nil {
		se, de = r.StartEnabled, r.DueEnabled
		if r.StartMinutes != nil {
			sm = *r.StartMinutes
		}
		if r.DueMinutes != nil {
			dm = *r.DueMinutes
		}
		if r.Timezone != "" {
			c.Timezone = r.Timezone
		}
		if r.BaseFingerprint == Fingerprint(t) {
			if r.Start != nil {
				start = r.Start
			}
			if r.Due != nil {
				due = r.Due
			}
		} else {
			stale = true
		}
	}
	specs := []Spec{}
	loc, _ := time.LoadLocation(c.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	binding := Hash(c.GatewayURL + "\n" + c.ChannelID)
	if m["recurring"] == true {
		se, de = false, false
	}
	for _, item := range []struct {
		kind, label string
		target      *time.Time
		minutes     int
		enabled     bool
	}{{"start", "任务即将开始", start, sm, se}, {"due", "任务即将截止", due, dm, de}} {
		if item.target == nil || !item.enabled {
			continue
		}
		target := item.target.UTC()
		fire := target.Add(-time.Duration(item.minutes) * time.Minute)
		expires := target
		if item.minutes == 0 {
			expires = target.Add(5 * time.Minute)
		}
		body := fmt.Sprintf("%s：%s；提前 %d 分钟。", item.label, target.In(loc).Format("2006-01-02 15:04:05 MST"), item.minutes)
		specs = append(specs, Spec{item.kind, target, fire, expires, item.label + "：" + t.Title, body, Severity(t.Importance, c.PriorityMap), binding})
	}
	if c.CreatedPush && !t.CreatedAt.IsZero() && !Occurrence(t) {
		specs = append(specs, Spec{"created", t.CreatedAt, t.CreatedAt, t.CreatedAt.Add(5 * time.Minute), "新任务：" + t.Title, "任务已创建。", Severity(t.Importance, c.PriorityMap), binding})
	}
	if stale {
		return specs, "confirmation_stale"
	}
	if len(specs) == 0 {
		return specs, "awaiting_precise_time"
	}
	return specs, "active"
}
func OpID(project, task string, s Spec) string {
	return "paca_" + Hash(strings.Join([]string{project, task, s.Kind, s.Target.UTC().Format(time.RFC3339Nano), s.Binding}, "\n"))
}

func Precise(t Task, kind string, value *time.Time) *time.Time { return precision(t, kind, value) }

// Occurrence identities are explicit on the first core create, before events fire.
func Occurrence(t Task) bool {
	m := Meta(t)
	extra, _ := t.Custom["_task_sync_v1"].(map[string]any)
	ref, _ := t.Custom["_integration_ref_v1"].(string)
	return strings.HasPrefix(ref, "period:") || m["occurrence_date"] != nil && m["occurrence_date"] != "" || extra["occurrence_date"] != nil && extra["occurrence_date"] != "" || extra["recurrence_parent"] != nil && extra["recurrence_parent"] != ""
}
func CreationKey(t Task) string {
	if ref, ok := t.Custom["_integration_ref_v1"].(string); ok && ref != "" {
		return "ref:" + ref
	}
	return "task:" + t.ID
}
