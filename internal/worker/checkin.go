package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model"
	"io"
	"net/http"
	"net/url"
	"time"
)

type checkinPlan struct {
	Enabled      bool       `json:"enabled"`
	Instance     string     `json:"instance_id"`
	Revision     int        `json:"revision"`
	Start        *time.Time `json:"start"`
	Due          *time.Time `json:"due"`
	StartMinutes int        `json:"start_minutes"`
	DueMinutes   int        `json:"due_minutes"`
	Ended        bool       `json:"end_succeeded"`
	State        string     `json:"state"`
}

func (w *Worker) checkinCall(ctx context.Context, path string, input any, out any) error {
	if w.CheckinURL == "" || w.CheckinSecret == "" {
		return errors.New("打卡服务尚未连接，请联系管理员完成配置")
	}
	if err := w.control(ctx); err != nil {
		return err
	}
	raw, _ := json.Marshal(input)
	req, err := http.NewRequestWithContext(ctx, "POST", w.CheckinURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.CheckinSecret)
	r, err := w.HTTP.Do(req)
	if err != nil {
		return errors.New("打卡服务暂时不可用，提醒会在有效期内重试")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		var reason struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&reason)
		return fmt.Errorf("check-in HTTP %d: %.300s", r.StatusCode, reason.Error)
	}
	if json.NewDecoder(io.LimitReader(r.Body, 2*1024*1024)).Decode(out) != nil {
		return errors.New("打卡安排暂时无法读取，请稍后重试")
	}
	return nil
}
func (w *Worker) specifications(ctx context.Context, s settings, t model.Task, rule *model.Rule, done bool) ([]model.Spec, string, error) {
	if !s.Config.Enabled || rule != nil && !rule.Enabled {
		specs, state := model.Compute(t, s.Config, rule, done)
		return specs, state, nil
	}
	base,state:=model.Compute(t,s.Config,rule,done)
	if model.Meta(t)["recurring"]==true {return base,state,nil}
	var plan checkinPlan
	if s.Config.CheckinEnabled {
		if err := w.checkinCall(ctx, "/internal/v1/task", map[string]any{"project_id": s.Project, "task_id": t.ID}, &plan); err != nil {
			created:=[]model.Spec{}
			for _,spec:=range base {if spec.Kind=="created" {created=append(created,spec)}}
			if len(created)>0 {return created,"checkin_unavailable",nil}
			return nil, "checkin_unavailable", err
		}
		if plan.Enabled {
			if plan.Instance == "" || plan.Revision < 1 || (plan.State != "active" && plan.State != "frozen") {
				return nil, "checkin_unavailable", errors.New("打卡安排已变更，请重新确认")
			}
			se, de := true, true
			if rule != nil {
				se = rule.StartEnabled
				de = rule.DueEnabled
			}
			rule = &model.Rule{Enabled: true, Timezone: s.Config.Timezone, StartEnabled: plan.Start != nil && se, DueEnabled: plan.Due != nil && !plan.Ended && de, Start: plan.Start, Due: plan.Due, StartMinutes: &plan.StartMinutes, DueMinutes: &plan.DueMinutes, BaseFingerprint: model.Fingerprint(t)}
			if plan.Ended {
				done = false
			}
		}
	}
	specs, state := model.Compute(t, s.Config, rule, done)
	if plan.Enabled {
		for i := range specs {
			if specs[i].Kind != "created" {
				specs[i].Binding += fmt.Sprintf(":checkin:%s:%d", plan.Instance, plan.Revision)
			}
		}
	}
	return specs, state, nil
}
func (w *Worker) resolveAction(ctx context.Context, j job) ([]byte, error) {
	var action struct {
		Enabled  bool              `json:"enabled"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := w.checkinCall(ctx, "/internal/v1/action", map[string]any{"project_id": j.Project, "task_id": j.Task, "kind": j.Kind, "target": j.Target}, &action); err != nil {
		return nil, err
	}
	var body map[string]any
	if json.Unmarshal(j.Payload, &body) != nil {
		return nil, errors.New("invalid persisted message")
	}
	if !action.Enabled {
		return nil, errors.New("打卡入口已失效，请重新确认任务安排")
	}
	if action.Enabled {
		u, err := url.Parse(action.Metadata["action_url"])
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || action.Metadata["action_version"] != "1" || action.Metadata["action_kind"] != "web" || len(u.String()) > 4096 {
			return nil, errors.New("打卡入口暂时无法打开，请稍后重试")
		}
		// This snapshot is saved before the first submission and is reused unchanged on retry.
		old, _ := body["metadata"].(map[string]any)
		if old == nil {
			old = map[string]any{}
		}
		for k, v := range action.Metadata {
			old[k] = v
		}
		body["metadata"] = old
	}
	return json.Marshal(body)
}
