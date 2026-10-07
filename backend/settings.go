package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	plugin "github.com/Paca-AI/plugin-sdk-go"
	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (p *integrationPlugin) audit(req *plugin.Request, action, subject string) {
	_, _ = p.db.Exec("INSERT INTO audit_log(project_id,actor_id,action,subject) VALUES($1,$2,$3,$4)", req.PathParam("projectId"), req.Caller.UserID, action, subject)
}
func (p *integrationPlugin) settings(req *plugin.Request, res *plugin.Response) {
	rows, err := p.db.Query("SELECT config::text,revision,COALESCE(last_reconciled::text,''),last_error FROM project_settings WHERE project_id=$1", req.PathParam("projectId"))
	if err != nil {
		res.Error(503, "settings unavailable")
		return
	}
	if len(rows.Rows) == 0 {
		res.JSON(200, map[string]any{"configured": false, "config": model.DefaultConfig(), "revision": 0})
		return
	}
	var cfg model.Config
	if json.Unmarshal([]byte(fmt.Sprint(rows.Rows[0][0])), &cfg) != nil {
		res.Error(503, "configuration invalid")
		return
	}
	res.JSON(200, map[string]any{"configured": true, "config": cfg, "revision": rows.Rows[0][1], "last_reconciled": rows.Rows[0][2], "last_error": rows.Rows[0][3]})
}
func (p *integrationPlugin) saveSettings(req *plugin.Request, res *plugin.Response) {
	input := struct {
		model.Config
		Password *string `json:"password"`
		Revision int     `json:"revision"`
	}{Config: model.DefaultConfig()}
	if json.Unmarshal(req.Body, &input) != nil || !model.ValidConfig(input.Config) || input.Revision < 0 {
		res.Error(400, "invalid gateway, channel, timezone or reminder rule")
		return
	}
	cipher := ""
	if input.Password != nil && *input.Password != "" {
		var err error
		cipher, err = p.encrypt(*input.Password)
		if err != nil {
			res.Error(503, "encryption unavailable")
			return
		}
	} else {
		rows, err := p.db.Query("SELECT password_enc FROM project_settings WHERE project_id=$1", req.PathParam("projectId"))
		if err != nil || len(rows.Rows) != 1 {
			res.Error(400, "channel password required for initial binding")
			return
		}
		cipher = fmt.Sprint(rows.Rows[0][0])
	}
	cfg, _ := json.Marshal(input.Config)
	n, err := p.db.Exec("INSERT INTO project_settings(project_id,config,password_enc) SELECT $1,$2::jsonb,$3 WHERE $4=0 ON CONFLICT(project_id) DO NOTHING", req.PathParam("projectId"), string(cfg), cipher, input.Revision)
	if input.Revision > 0 {
		n, err = p.db.Exec("UPDATE project_settings SET config=$1::jsonb,password_enc=$2,revision=revision+1,needs_reconcile=TRUE,updated_at=NOW() WHERE project_id=$3 AND revision=$4", string(cfg), cipher, req.PathParam("projectId"), input.Revision)
	}
	if err != nil {
		res.Error(503, "binding persistence failed")
		return
	}
	if n != 1 {
		res.Error(409, "settings changed; reload before saving")
		return
	}
	p.audit(req, "binding.saved", input.ChannelID)
	res.JSON(200, map[string]any{"revision": input.Revision + 1})
}
func (p *integrationPlugin) reminders(req *plugin.Request, res *plugin.Response) {
	rows, err := p.db.Query("SELECT config::text,revision FROM task_rules WHERE project_id=$1 AND task_id=$2", req.PathParam("projectId"), req.PathParam("taskId"))
	if err != nil {
		res.Error(503, "reminder rule unavailable")
		return
	}
	var rule any
	revision := any(0)
	if len(rows.Rows) == 1 {
		_ = json.Unmarshal([]byte(fmt.Sprint(rows.Rows[0][0])), &rule)
		revision = rows.Rows[0][1]
	}
	plans, err := p.db.Query("SELECT CASE WHEN p.updated_at < COALESCE((SELECT r.updated_at FROM task_rules r WHERE r.project_id=p.project_id AND r.task_id=p.task_id),p.updated_at) THEN 'awaiting_reconciliation' ELSE p.state END,p.revision,p.updated_at::text FROM plans p WHERE p.project_id=$1 AND p.task_id=$2", req.PathParam("projectId"), req.PathParam("taskId"))
	if err != nil {
		res.Error(503, "plan unavailable")
		return
	}
	plan := any(nil)
	if rule != nil {
		plan = map[string]any{"state": "awaiting_reconciliation"}
	}
	if len(plans.Rows) == 1 {
		r := plans.Rows[0]
		plan = map[string]any{"state": r[0], "revision": r[1], "updated_at": r[2]}
	}
	res.JSON(200, map[string]any{"rule": rule, "revision": revision, "plan": plan})
}
func (p *integrationPlugin) setReminders(req *plugin.Request, res *plugin.Response) {
	input := struct {
		Revision     int        `json:"revision"`
		Enabled      bool       `json:"enabled"`
		Timezone     string     `json:"timezone"`
		Start        string     `json:"start"`
		Due          string     `json:"due"`
		StartEnabled bool       `json:"start_enabled"`
		DueEnabled   bool       `json:"due_enabled"`
		StartMinutes *int       `json:"start_minutes"`
		DueMinutes   *int       `json:"due_minutes"`
		BaseTask     model.Task `json:"base_task"`
	}{Enabled: true, StartEnabled: true, DueEnabled: true, Timezone: "Asia/Shanghai"}
	if json.Unmarshal(req.Body, &input) != nil || input.Revision < 0 || input.BaseTask.ID != req.PathParam("taskId") || input.BaseTask.ProjectID != req.PathParam("projectId") || !uuidPattern.MatchString(input.BaseTask.ID) {
		res.Error(400, "current core task snapshot and base rule revision required")
		return
	}
	for _, v := range []*int{input.StartMinutes, input.DueMinutes} {
		if v != nil && (*v < 0 || *v > 1440) {
			res.Error(400, "minutes must be 0–1440")
			return
		}
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil || input.Timezone == "" {
		res.Error(400, "valid IANA timezone required")
		return
	}
	start, err := model.ParseTime(input.Start, input.Timezone)
	if err != nil {
		res.Error(400, err.Error())
		return
	}
	due, err := model.ParseTime(input.Due, input.Timezone)
	if err != nil {
		res.Error(400, err.Error())
		return
	}
	if start != nil && due != nil && due.Before(*start) {
		res.Error(400, "due time cannot precede start time")
		return
	}
	rule := model.Rule{Enabled: input.Enabled, Timezone: input.Timezone, StartEnabled: input.StartEnabled, DueEnabled: input.DueEnabled, Start: start, Due: due, StartMinutes: input.StartMinutes, DueMinutes: input.DueMinutes, BaseFingerprint: model.Fingerprint(input.BaseTask)}
	cfg, _ := json.Marshal(rule)
	n, err := p.db.Exec("INSERT INTO task_rules(project_id,task_id,config) SELECT $1,$2,$3::jsonb WHERE $4=0 ON CONFLICT(project_id,task_id) DO NOTHING", req.PathParam("projectId"), req.PathParam("taskId"), string(cfg), input.Revision)
	if input.Revision > 0 {
		n, err = p.db.Exec("UPDATE task_rules SET config=$1::jsonb,revision=revision+1,updated_at=NOW() WHERE project_id=$2 AND task_id=$3 AND revision=$4", string(cfg), req.PathParam("projectId"), req.PathParam("taskId"), input.Revision)
	}
	if err != nil {
		res.Error(503, "rule persistence failed; configure project channel first")
		return
	}
	if n != 1 {
		res.Error(409, "rule changed; reload before saving")
		return
	}
	_, _ = p.db.Exec("UPDATE project_settings SET needs_reconcile=TRUE WHERE project_id=$1", req.PathParam("projectId"))
	p.audit(req, "reminder.configured", req.PathParam("taskId"))
	res.JSON(202, map[string]any{"revision": input.Revision + 1, "state": "awaiting_reconciliation"})
}
func (p *integrationPlugin) jobs(req *plugin.Request, res *plugin.Response) {
	taskID := req.PathParam("taskId")
	rows, err := p.db.Query("SELECT id,task_id::text,kind,fire_at::text,expires_at::text,state,attempts,last_error,op_id FROM jobs WHERE project_id=$1 AND ($2='' OR task_id::text=$2) ORDER BY id DESC LIMIT 100", req.PathParam("projectId"), taskID)
	if err != nil {
		res.Error(503, "queue unavailable")
		return
	}
	items := []any{}
	for _, r := range rows.Rows {
		items = append(items, map[string]any{"id": r[0], "task_id": r[1], "kind": r[2], "fire_at": r[3], "expires_at": r[4], "state": r[5], "attempts": r[6], "error": r[7], "op_id": r[8]})
	}
	res.JSON(200, map[string]any{"items": items, "accepted_does_not_mean_phone_delivered": true})
}
func (p *integrationPlugin) retry(req *plugin.Request, res *plugin.Response) {
	n, err := p.db.Exec("UPDATE jobs SET state='retry_wait',next_attempt=NOW(),last_error='',lease_owner=NULL,lease_until=NULL WHERE id=$1 AND project_id=$2 AND state IN ('failed','retry_wait') AND expires_at>NOW() AND EXISTS(SELECT 1 FROM plans p WHERE p.project_id=jobs.project_id AND p.task_id=jobs.task_id AND p.revision=jobs.plan_revision AND p.state IN ('active','confirmation_stale'))", req.PathParam("jobId"), req.PathParam("projectId"))
	if err != nil {
		res.Error(503, "retry persistence failed")
		return
	}
	if n != 1 {
		res.Error(409, "job expired, inactive or already accepted")
		return
	}
	p.audit(req, "job.retry", req.PathParam("jobId"))
	res.JSON(202, map[string]any{"queued": true, "op_id_preserved": true})
}
func (p *integrationPlugin) dirty(evt *plugin.Event) {
	// Events are hints. Periodic complete REST reconciliation remains authoritative.
	_, _ = p.db.Exec("UPDATE project_settings SET needs_reconcile=TRUE")
}
