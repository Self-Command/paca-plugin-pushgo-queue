package model

import (
	"errors"
	"strings"
)

type ReminderFlags struct {
 Revision int `json:"revision"`
 Enabled bool `json:"enabled"`
 StartEnabled bool `json:"start_enabled"`
 DueEnabled bool `json:"due_enabled"`
}
type TaskOperation struct {
	Reminders *ReminderFlags `json:"reminders,omitempty"`
 OpID string `json:"op_id"`
	Kind string `json:"kind"`
	TaskID string `json:"task_id"`
	BaseVersion string `json:"base_version"`
	Title *string `json:"title,omitempty"`
	Content *string `json:"content,omitempty"`
	StatusID *string `json:"status_id,omitempty"`
	Importance *int `json:"importance,omitempty"`
	Tags *[]string `json:"tags,omitempty"`
	Times *TaskTimes `json:"times,omitempty"`
}

func (o TaskOperation) Validate() error {
 if o.Reminders!=nil && (o.Reminders.Revision<0||o.Kind!="update") {return errors.New("提醒版本无效。")}
	if len(o.OpID)<8||len(o.OpID)>128||strings.ContainsAny(o.OpID,"/\\\n\r") {return errors.New("操作标识无效。")}
	if o.Kind!="create"&&o.Kind!="update" {return errors.New("请选择创建或修改任务。")}
	if o.Kind=="create"&&(o.Title==nil||strings.TrimSpace(*o.Title)==""||o.TaskID!="") {return errors.New("请输入任务标题。")}
	if o.Kind=="update"&&(o.TaskID==""||len(o.BaseVersion)!=64) {return errors.New("请刷新任务后再修改。")}
	if o.Title!=nil&&(len(strings.TrimSpace(*o.Title))==0||len(*o.Title)>1000){return errors.New("任务标题不能为空或过长。")}
	if o.Content!=nil&&len(*o.Content)>65536{return errors.New("任务内容过长，请缩短后保存。")}
	if o.Importance!=nil&&(*o.Importance<0||*o.Importance>200){return errors.New("任务优先级无效。")}
	if o.Tags!=nil {if len(*o.Tags)>100{return errors.New("任务标签过多。")};for _,tag:=range *o.Tags {if len(tag)>200{return errors.New("任务标签过长。")}}}
	if o.Times!=nil {if _,err:=TimePatch(Task{},*o.Times);err!=nil{return err}}
	return nil
}
