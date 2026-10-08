package worker

import (
	"context"
	"encoding/json"
	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/buildinfo"
	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOptionalCheckinIsolatedAndEndDoesNotCancelIndependentStart(t *testing.T) {
	start := time.Now().UTC().Add(time.Hour)
	due := start.Add(time.Hour)
	target := start
	actionURL := "https://task.example.org/checkin/card/start#token=limited"
	available := true
	internal := 0
	host := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/plugins/"+PluginID+"/worker/control" {
			json.NewEncoder(out).Encode(map[string]any{"id": PluginID, "version": Version, "source_sha": buildinfo.SourceSHA, "schema_version": 6, "enabled": true})
			return
		}
		internal++
		if r.Header.Get("Authorization") != "Bearer runtime-only" {
			t.Error("binding credential absent")
		}
		if !available {
			out.WriteHeader(503)
			return
		}
		if r.URL.Path == "/internal/v1/task" {
			json.NewEncoder(out).Encode(checkinPlan{Enabled: true, Instance: "tested-instance", Revision: 2, Start: &start, Due: &due, StartMinutes: 10, DueMinutes: 10, Ended: true, State: "frozen"})
			return
		}
		var input struct {
			Target time.Time `json:"target"`
		}
		json.NewDecoder(r.Body).Decode(&input)
		if !input.Target.Equal(target) {
			t.Error("action target changed")
		}
		json.NewEncoder(out).Encode(map[string]any{"enabled": true, "metadata": map[string]string{"action_version": "1", "action_kind": "web", "action_url": actionURL, "action_label": "去打卡"}})
	}))
	defer host.Close()
	w := &Worker{API: host.URL, Secret: "host-secret", HTTP: host.Client(), CheckinURL: host.URL, CheckinSecret: "runtime-only"}
	task := model.Task{ID: "t", Title: "两张卡独立", Custom: map[string]any{}}
	cfg := model.DefaultConfig()
	cfg.Enabled = true
	_, _, err := w.specifications(context.Background(), settings{Project: "p", Config: cfg}, task, nil, false)
	if err != nil || internal != 0 {
		t.Fatal("plain reminders invoked C")
	}
	cfg.CheckinEnabled = true
	specs, state, err := w.specifications(context.Background(), settings{Project: "p", Config: cfg}, task, nil, true)
	if err != nil || state != "active" || len(specs) != 1 || specs[0].Kind != "start" {
		t.Fatalf("end completion incorrectly cancelled start: %v %s %v", err, state, specs)
	}
	j := job{Project: "p", Task: "t", Kind: "start", Target: target, Payload: []byte(`{"title":"提醒","metadata":{"task_card_version":"1"}}`)}
	raw, err := w.resolveAction(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	json.Unmarshal(raw, &snapshot)
	metadata := snapshot["metadata"].(map[string]any)
	if metadata["task_card_version"] != "1" || metadata["action_url"] != actionURL {
		t.Fatal("task card or web action lost")
	}
	actionURL = "javascript:alert(1)"
	if _, err = w.resolveAction(context.Background(), j); err == nil {
		t.Fatal("dangerous web action accepted")
	}
	available = false
	if _, _, err = w.specifications(context.Background(), settings{Project: "p", Config: cfg}, task, nil, false); err == nil {
		t.Fatal("C outage silently emitted ordinary reminder")
	}
}
