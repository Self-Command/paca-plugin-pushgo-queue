package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	plugin "github.com/Paca-AI/plugin-sdk-go"
	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model"
)

func (p *integrationPlugin) taskAuthorization(req *plugin.Request, res *plugin.Response) {
	res.JSON(200, map[string]any{"authorized": true, "project_id": req.PathParam("projectId"), "actor_id": req.Caller.UserID})
}

func (p *integrationPlugin) submitTaskOperation(req *plugin.Request, res *plugin.Response) {
	var operation model.TaskOperation
	decoder := json.NewDecoder(strings.NewReader(string(req.Body)))
	decoder.DisallowUnknownFields()
	if len(req.Body) > 131072 || decoder.Decode(&operation) != nil || decoder.Decode(new(any)) != io.EOF {
		res.Error(400, "请检查任务内容和时间设置。")
		return
	}
	if err := operation.Validate(); err != nil {
		res.Error(400, err.Error())
		return
	}
	if operation.Times != nil {
		rows, err := p.db.Query("SELECT COALESCE(config->>'checkin_enabled','false') FROM project_settings WHERE project_id=$1", req.PathParam("projectId"))
		if err != nil { res.Error(503, "打卡设置暂时无法核对。"); return }
		checkin := len(rows.Rows) == 1 && fmt.Sprint(rows.Rows[0][0]) == "true"
		if err := operation.Times.ValidateCheckinLead(checkin); err != nil { res.Error(400, err.Error()); return }
	}
	if operation.TaskID != "" && !uuidPattern.MatchString(operation.TaskID) || operation.StatusID != nil && *operation.StatusID != "" && !uuidPattern.MatchString(*operation.StatusID) {
		res.Error(400, "任务或状态标识无效。")
		return
	}
	if req.Caller.UserID == "" {
		res.Error(401, "请先登录。")
		return
	}
	if operation.Kind == "update" {
		versions, lookupErr := p.db.Query("SELECT version FROM task_time_views WHERE project_id=$1 AND task_id=$2", req.PathParam("projectId"), operation.TaskID)
		if lookupErr != nil {
			res.Error(503, "任务版本暂时无法读取。")
			return
		}
		if len(versions.Rows) == 1 && fmt.Sprint(versions.Rows[0][0]) != operation.BaseVersion {
			existing, _ := p.db.Query("SELECT body_hash FROM task_operations WHERE project_id=$1 AND op_id=$2", req.PathParam("projectId"), operation.OpID)
			if len(existing.Rows) == 0 {
				res.Error(409, "任务已发生变化，请刷新后重新确认。")
				return
			}
		}
	}
	credentials := map[string]string{}
	for header, value := range req.Headers {
		key := strings.ToLower(header)
		if key == "x-api-key" || key == "authorization" || key == "cookie" {
			credentials[key] = value
		}
	}
	if len(credentials) == 0 {
		res.Error(401, "登录信息已失效，请重新登录。")
		return
	}
	rawCredentials, _ := json.Marshal(credentials)
	sealed, err := p.encrypt(string(rawCredentials))
	if err != nil {
		res.Error(503, "任务暂时无法安全保存。")
		return
	}
	raw, _ := json.Marshal(operation)
	hash := model.Hash(operation)
	rows, err := p.db.Query("INSERT INTO task_operations(project_id,op_id,actor_id,body_hash,body,auth_enc) VALUES($1,$2,$3,$4,$5::jsonb,$6) ON CONFLICT(project_id,op_id) DO UPDATE SET op_id=EXCLUDED.op_id RETURNING body_hash,state,task_id::text", req.PathParam("projectId"), operation.OpID, req.Caller.UserID, hash, string(raw), sealed)
	if err != nil || len(rows.Rows) != 1 {
		res.Error(503, "任务操作暂时无法保存。")
		return
	}
	if fmt.Sprint(rows.Rows[0][0]) != hash {
		res.Error(409, "相同操作标识不能提交不同内容。")
		return
	}
	p.audit(req, "task.operation", operation.OpID)
	res.JSON(202, map[string]any{"op_id": operation.OpID, "state": rows.Rows[0][1], "task_id": rows.Rows[0][2]})
}

func (p *integrationPlugin) taskOperation(req *plugin.Request, res *plugin.Response) {
	opID, decodeErr := url.PathUnescape(req.PathParam("opId"))
	if decodeErr != nil {
		res.Error(400, "操作标识无效。")
		return
	}
	rows, err := p.db.Query("SELECT state,task_id::text,result::text,error FROM task_operations WHERE project_id=$1 AND op_id=$2", req.PathParam("projectId"), opID)
	if err != nil {
		res.Error(503, "任务操作暂时无法读取。")
		return
	}
	if len(rows.Rows) != 1 {
		res.Error(404, "任务操作未找到。")
		return
	}
	row := rows.Rows[0]
	var result any
	if row[2] != nil {
		_ = json.Unmarshal([]byte(fmt.Sprint(row[2])), &result)
	}
	res.JSON(200, map[string]any{"op_id": opID, "state": row[0], "task_id": row[1], "result": result, "error": row[3]})
}

func (p *integrationPlugin) taskTimes(req *plugin.Request, res *plugin.Response) {
	_, _ = p.db.Exec("INSERT INTO task_time_requests(project_id,task_id) VALUES($1,$2) ON CONFLICT DO NOTHING", req.PathParam("projectId"), req.PathParam("taskId"))
	rows, err := p.db.Query("SELECT version,times::text,frozen,error,updated_at::text FROM task_time_views WHERE project_id=$1 AND task_id=$2", req.PathParam("projectId"), req.PathParam("taskId"))
	if err != nil {
		res.Error(503, "任务时间暂时无法读取。")
		return
	}
	if len(rows.Rows) == 0 {
		res.JSON(202, map[string]any{"state": "pending", "message": "正在读取任务时间，请稍后刷新。"})
		return
	}
	row := rows.Rows[0]
	var times any
	_ = json.Unmarshal([]byte(fmt.Sprint(row[1])), &times)
	res.JSON(200, map[string]any{"state": "ready", "version": row[0], "times": times, "frozen": row[2], "error": row[3], "updated_at": row[4]})
}
