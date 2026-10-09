package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model"
	"github.com/jackc/pgx/v5"
)

func (w *Worker) callerCall(ctx context.Context, auth map[string]string, method, path string, body, out any) error {
	if err := w.control(ctx); err != nil {
		return err
	}
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.API+"/api/v1"+path, input)
	if err != nil {
		return err
	}
	for _, key := range []string{"x-api-key", "authorization", "cookie"} {
		if value := auth[key]; value != "" {
			req.Header.Set(key, value)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := w.HTTP.Do(req)
	if err != nil {
		return errors.New("任务请求结果尚未确认。")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return apiError{response.StatusCode}
	}
	if out == nil {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 8*1024*1024)).Decode(&envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, out)
}

func (w *Worker) cacheTaskTimes(ctx context.Context, project string, t model.Task, frozen *bool, message string) error {
	if w.DB == nil {
		return nil
	}
	raw, _ := json.Marshal(model.TimesOf(t))
	_, err := w.DB.Exec(ctx, "INSERT INTO task_time_views(project_id,task_id,version,times,frozen,error) VALUES($1,$2,$3,$4::jsonb,$5,$6) ON CONFLICT(project_id,task_id) DO UPDATE SET version=EXCLUDED.version,times=EXCLUDED.times,frozen=COALESCE(EXCLUDED.frozen,task_time_views.frozen),error=EXCLUDED.error,updated_at=clock_timestamp()", project, t.ID, model.TimeVersion(t), string(raw), frozen, message)
	return err
}

func (w *Worker) operationResult(ctx context.Context, project, op, state, message string, value any) error {
	raw, _ := json.Marshal(value)
	_, err := w.DB.Exec(ctx, "UPDATE task_operations SET state=$3,error=$4,result=$5::jsonb,task_id=COALESCE(NULLIF($5::jsonb->>'task_id','')::uuid,task_id),auth_enc=CASE WHEN $3 IN('applied','conflict','failed','uncertain') THEN '' ELSE auth_enc END,lease_until=NULL,updated_at=clock_timestamp(),next_attempt=NOW()+INTERVAL '10 seconds' WHERE project_id=$1 AND op_id=$2", project, op, state, message, string(raw))
	return err
}

func operationMarker(t model.Task, op string) bool {
	m, _ := t.Custom["_pushgo_operation_v1"].(map[string]any)
	return m["op_id"] == op
}

func (w *Worker) findTaskOperation(ctx context.Context, auth map[string]string, project, op string) ([]model.Task, error) {
	items := []model.Task{}
	cursor := ""
	for page := 0; page < 10000; page++ {
		var result struct {
			Items []model.Task `json:"items"`
			Next  *string      `json:"next_cursor"`
		}
		path := "/projects/" + project + "/tasks?page_size=200"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		if err := w.callerCall(ctx, auth, "GET", path, nil, &result); err != nil {
			return nil, err
		}
		for _, task := range result.Items {
			if operationMarker(task, op) {
				items = append(items, task)
			}
		}
		if result.Next == nil || *result.Next == "" {
			return items, nil
		}
		if *result.Next == cursor {
			return nil, errors.New("任务列表尚未完整读取。")
		}
		cursor = *result.Next
	}
	return nil, errors.New("任务列表过大，请分批核对。")
}

// The original caller's encrypted credentials are short lived and never returned
// to clients. Official permissions are rechecked immediately before task writes.
func (w *Worker) applyTaskOperation(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var project, op, state, sealed string
	var raw []byte
	var created time.Time
	var attempts int
	err := w.DB.QueryRow(ctx, "UPDATE task_operations SET lease_until=NOW()+INTERVAL '90 seconds' WHERE (project_id,op_id)=(SELECT project_id,op_id FROM task_operations WHERE state IN('pending','sending','retry') AND next_attempt<=NOW() AND (lease_until IS NULL OR lease_until<NOW()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING project_id::text,op_id,state,body,auth_enc,created_at,attempts").Scan(&project, &op, &state, &raw, &sealed, &created, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	defer w.DB.Exec(context.Background(), "UPDATE task_operations SET lease_until=NULL WHERE project_id=$1 AND op_id=$2", project, op)
	if time.Since(created) > 5*time.Minute {
		return w.operationResult(ctx, project, op, "uncertain", "登录授权已过期，请先核对创建结果，再重新提交。", nil)
	}
	plain, err := decryptPassword(sealed, w.EncryptionKey)
	if err != nil {
		return w.operationResult(ctx, project, op, "failed", "任务授权暂时无法读取，请重新登录。", nil)
	}
	auth := map[string]string{}
	if json.Unmarshal([]byte(plain), &auth) != nil {
		return errors.New("任务授权格式无效。")
	}
	// This plugin endpoint uses the host's current user/agent effective role.
	if err = w.callerCall(ctx, auth, "GET", "/plugins/"+PluginID+"/projects/"+project+"/task-authorization", nil, nil); err != nil {
		var api apiError
		if errors.As(err, &api) && (api.Code == 401 || api.Code == 403) {
			return w.operationResult(ctx, project, op, "failed", "你已无权创建或修改此项目任务。", nil)
		}
		return w.operationResult(ctx, project, op, "retry", "暂时无法确认任务权限，将稍后重试。", nil)
	}
	var request model.TaskOperation
	if json.Unmarshal(raw, &request) != nil {
		return errors.New("已保存的任务操作无效。")
	}
	root := "/projects/" + project + "/tasks"
	var current model.Task
	if request.Kind == "update" {
		if err = w.callerCall(ctx, auth, "GET", root+"/"+request.TaskID, nil, &current); err != nil {
			return w.operationResult(ctx, project, op, "failed", "任务已删除或不可访问。", nil)
		}
		if operationMarker(current, op) {
			if !operationMatches(current, request) {
				return w.operationResult(ctx, project, op, "conflict", "保存后任务发生了并发修改，请核对实际结果。", map[string]any{"status_code": 409, "task_id": current.ID})
			}
			return w.completeTaskOperation(ctx, project, op, current)
		}
		if model.TimeVersion(current) != request.BaseVersion {
			return w.operationResult(ctx, project, op, "conflict", "任务已发生变化，请刷新后核对时间。", map[string]any{"status_code": 409, "task_id": current.ID, "version": model.TimeVersion(current)})
		}
		checkConfig, configErr := w.loadSettings(ctx, project)
		if request.Times != nil && configErr == nil && checkConfig.Config.CheckinEnabled && w.CheckinURL != "" {
			var plan checkinPlan
			if err = w.checkinCall(ctx, "/internal/v1/times/freeze", map[string]string{"project_id": project, "task_id": current.ID}, &plan); err != nil {
				return w.operationResult(ctx, project, op, "retry", "打卡窗口暂时无法核对，请稍后重试。", nil)
			}
			if plan.Enabled && plan.Frozen {
				return w.operationResult(ctx, project, op, "conflict", "打卡窗口已开放，时间已冻结。请取消原实例后再改期。", map[string]any{"status_code": 409, "task_id": current.ID})
			}
		}
	} else if attempts > 0 {
		matches, findErr := w.findTaskOperation(ctx, auth, project, op)
		if findErr != nil {
			return w.operationResult(ctx, project, op, "sending", "正在核对任务创建结果。", nil)
		}
		if len(matches) == 1 && operationMatches(matches[0], request) {
			return w.completeTaskOperation(ctx, project, op, matches[0])
		}
		return w.operationResult(ctx, project, op, "uncertain", "创建结果需要核对，已停止重复创建。", map[string]any{"matches": len(matches)})
	}
	if request.Times != nil {
		settings, settingsErr := w.loadSettings(ctx, project)
		if settingsErr != nil && !errors.Is(settingsErr, pgx.ErrNoRows) {
			return settingsErr
		}
		if err := request.Times.ValidateCheckinLead(settings.Config.CheckinEnabled); err != nil {
			return w.operationResult(ctx, project, op, "failed", err.Error(), nil)
		}
	}
	if request.Reminders != nil {
		var revision int
		ruleErr := w.DB.QueryRow(ctx, "SELECT revision FROM task_rules WHERE project_id=$1 AND task_id=$2", project, request.TaskID).Scan(&revision)
		if ruleErr != nil && !errors.Is(ruleErr, pgx.ErrNoRows) {
			return ruleErr
		}
		if revision != request.Reminders.Revision {
			return w.operationResult(ctx, project, op, "conflict", "提醒设置已发生变化，请刷新后重试。", map[string]any{"status_code": 409, "task_id": request.TaskID})
		}
	}
	patch := map[string]any{}
	if request.Times != nil {
		patch, err = model.TimePatch(current, *request.Times)
		if err != nil {
			return w.operationResult(ctx, project, op, "failed", err.Error(), nil)
		}
	}
	if request.Title != nil {
		patch["title"] = strings.TrimSpace(*request.Title)
	}
	if request.Content != nil {
		blocks := []any{}
		for _, line := range strings.Split(*request.Content, "\n") {
			blocks = append(blocks, map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": line, "styles": map[string]any{}}}, "children": []any{}})
		}
		patch["description"] = blocks
	}
	if request.StatusID != nil && *request.StatusID != "" {
		patch["status_id"] = *request.StatusID
	}
	if request.Importance != nil {
		patch["importance"] = *request.Importance
	}
	if request.Tags != nil {
		patch["tags"] = *request.Tags
	}
	custom, _ := patch["custom_fields"].(map[string]any)
	if custom == nil {
		custom = map[string]any{}
		for key, value := range current.Custom {
			custom[key] = value
		}
	}
	custom["_pushgo_operation_v1"] = map[string]any{"op_id": op, "request_hash": model.Hash(request)}
	patch["custom_fields"] = custom
	// Persist the attempt before calling the official API; a crash does not turn
	// an uncertain creation into permission to create again.
	if _, err = w.DB.Exec(ctx, "UPDATE task_operations SET state='sending',attempts=attempts+1 WHERE project_id=$1 AND op_id=$2", project, op); err != nil {
		return err
	}
	method, path := "POST", root
	if request.Kind == "update" {
		method, path = "PATCH", root+"/"+current.ID
	}
	err = w.callerCall(ctx, auth, method, path, patch, &current)
	if err != nil {
		var api apiError
		if errors.As(err, &api) && (api.Code == 400 || api.Code == 401 || api.Code == 403 || api.Code == 404 || api.Code == 422) {
			return w.operationResult(ctx, project, op, "failed", "任务内容、状态或权限无效，请核对后重试。", nil)
		}
		return w.operationResult(ctx, project, op, "sending", "请求结果尚未确认，正在核对稳定标记。", nil)
	}
	if current.ID == "" {
		return w.operationResult(ctx, project, op, "sending", "请求结果尚未确认，正在核对稳定标记。", nil)
	}
	var verified model.Task
	if err = w.callerCall(ctx, auth, "GET", root+"/"+current.ID, nil, &verified); err != nil {
		return w.operationResult(ctx, project, op, "sending", "任务已提交，正在核对实际结果。", nil)
	}
	if !operationMarker(verified, op) || !operationMatches(verified, request) {
		return w.operationResult(ctx, project, op, "uncertain", "任务在保存期间发生变化，请核对实际结果。", map[string]any{"task_id": current.ID, "before": current, "expected": patch, "actual": verified})
	}
	return w.completeTaskOperation(ctx, project, op, verified)
}

func (w *Worker) completeTaskOperation(ctx context.Context, project, op string, t model.Task) error {
	var request model.TaskOperation
	var saved []byte
	if err := w.DB.QueryRow(ctx, "SELECT body FROM task_operations WHERE project_id=$1 AND op_id=$2", project, op).Scan(&saved); err != nil {
		return err
	}
	if err := json.Unmarshal(saved, &request); err != nil {
		return err
	}
	if request.Reminders != nil {
		flags := request.Reminders
		times := model.TimesOf(t)
		rule := model.Rule{Enabled: flags.Enabled, StartEnabled: flags.StartEnabled, DueEnabled: flags.DueEnabled, Timezone: times.Timezone, BaseFingerprint: model.Fingerprint(t)}
		raw, _ := json.Marshal(rule)
		var existing []byte
		_ = w.DB.QueryRow(ctx, "SELECT config FROM task_rules WHERE project_id=$1 AND task_id=$2", project, t.ID).Scan(&existing)
		var existingRule model.Rule
		_ = json.Unmarshal(existing, &existingRule)
		if model.Hash(existingRule) != model.Hash(rule) {
			tag, err := w.DB.Exec(ctx, "INSERT INTO task_rules(project_id,task_id,config) SELECT $1,$2,$3::jsonb WHERE $4=0 OR EXISTS(SELECT 1 FROM task_rules WHERE project_id=$1 AND task_id=$2 AND revision=$4) ON CONFLICT(project_id,task_id) DO UPDATE SET config=EXCLUDED.config,revision=task_rules.revision+1,updated_at=clock_timestamp() WHERE task_rules.revision=$4", project, t.ID, string(raw), flags.Revision)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return w.operationResult(ctx, project, op, "conflict", "任务时间已保存，但提醒设置发生了变化，请核对。", map[string]any{"status_code": 409, "task_id": t.ID})
			}
		}
	}
	var frozen *bool
	if w.CheckinURL == "" {
		value := false
		frozen = &value
	}
	if err := w.cacheTaskTimes(ctx, project, t, frozen, ""); err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, "UPDATE task_rules SET config=(config-'start'-'due')||jsonb_build_object('base_fingerprint',$3::text),updated_at=clock_timestamp() WHERE project_id=$1 AND task_id=$2", project, t.ID, model.Fingerprint(t)); err != nil {
		return err
	}
	if _, err := w.DB.Exec(ctx, "UPDATE project_settings SET needs_reconcile=TRUE WHERE project_id=$1", project); err != nil {
		return err
	}
	return w.operationResult(ctx, project, op, "applied", "", map[string]any{"task_id": t.ID, "version": model.TimeVersion(t), "times": model.TimesOf(t)})
}

func operationMatches(t model.Task, o model.TaskOperation) bool {
	if o.Title != nil && t.Title != strings.TrimSpace(*o.Title) {
		return false
	}
	if o.StatusID != nil && *o.StatusID != "" && t.StatusID != *o.StatusID {
		return false
	}
	if o.Importance != nil && t.Importance != *o.Importance {
		return false
	}
	if o.Tags != nil && model.Hash(t.Tags) != model.Hash(*o.Tags) {
		return false
	}
	if o.Times != nil {
		patch, err := model.TimePatch(t, *o.Times)
		if err != nil {
			return false
		}
		raw, _ := json.Marshal(patch)
		var expected model.Task
		if json.Unmarshal(raw, &expected) != nil {
			return false
		}
		if model.Hash(model.TimesOf(expected)) != model.Hash(model.TimesOf(t)) {
			return false
		}
	}
	if o.Content != nil {
		var lines []string
		raw, _ := json.Marshal(t.Description)
		var blocks []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &blocks) != nil {
			return false
		}
		for _, b := range blocks {
			var line string
			for _, c := range b.Content {
				line += c.Text
			}
			lines = append(lines, line)
		}
		if strings.Join(lines, "\n") != *o.Content {
			return false
		}
	}
	return true
}
func (w *Worker) refreshTaskTime(ctx context.Context) error {
	var project, task string
	err := w.DB.QueryRow(ctx, "DELETE FROM task_time_requests WHERE (project_id,task_id)=(SELECT project_id,task_id FROM task_time_requests ORDER BY requested_at LIMIT 1) RETURNING project_id::text,task_id::text").Scan(&project, &task)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var t model.Task
	if err = w.call(ctx, "GET", "/projects/"+project+"/tasks/"+task, nil, &t); err != nil {
		return err
	}
	frozen := false
	settings, settingsErr := w.loadSettings(ctx, project)
	if settingsErr == nil && settings.Config.CheckinEnabled {
		var state checkinPlan
		if err = w.checkinCall(ctx, "/internal/v1/times/freeze", map[string]string{"project_id": project, "task_id": task}, &state); err != nil {
			return w.cacheTaskTimes(ctx, project, t, nil, "打卡窗口暂时无法核对，保存时会重新检查。")
		}
		frozen = state.Enabled && state.Frozen
	}
	return w.cacheTaskTimes(ctx, project, t, &frozen, "")
}
