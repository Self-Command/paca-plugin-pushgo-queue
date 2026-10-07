package worker

import (
	"context"
	"encoding/json"
	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model"
	"strings"
)

type TaskCard struct {
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Start    any      `json:"start"`
	Due      any      `json:"due"`
	Priority string   `json:"priority"`
	Status   string   `json:"status"`
	Tags     []string `json:"tags"`
	Source   string   `json:"source"`
	Timezone string   `json:"timezone"`
}

func plainBlocks(value any) string {
	var lines []string
	var walk func(any)
	walk = func(v any) {
		switch item := v.(type) {
		case []any:
			for _, child := range item {
				walk(child)
			}
		case map[string]any:
			if text, ok := item["text"].(string); ok {
				lines = append(lines, text)
			}
			if c, ok := item["content"]; ok {
				before := len(lines)
				walk(c)
				if len(lines) > before && item["type"] == "paragraph" {
					lines = append(lines, "\n")
				}
			}
			if c, ok := item["children"]; ok {
				walk(c)
			}
		case string:
			lines = append(lines, item)
		}
	}
	walk(value)
	return strings.TrimSpace(strings.Join(lines, ""))
}
func priorityLabel(value int) string {
	switch {
	case value >= 100:
		return "紧急"
	case value >= 50:
		return "高"
	case value >= 20:
		return "中"
	case value > 0:
		return "低"
	}
	return "未设置"
}
func statusLabel(name, category string) string {
	defaults := map[string]string{"Backlog": "待安排", "Todo": "待完成", "To Do": "待完成", "In Progress": "进行中", "Done": "已完成", "Archive": "已归档"}
	if value, ok := defaults[name]; ok {
		return value
	}
	if name != "" {
		return name
	}
	labels := map[string]string{"backlog": "待安排", "todo": "待完成", "inprogress": "进行中", "done": "已完成"}
	if value, ok := labels[category]; ok {
		return value
	}
	return "未设置"
}
func (w *Worker) taskCardMetadata(ctx context.Context, t model.Task, cfg model.Config, rule *model.Rule, done bool) (map[string]string, error) {
	start, due := model.Precise(t, "start", t.Start), model.Precise(t, "due", t.Due)
	if rule != nil && rule.BaseFingerprint == model.Fingerprint(t) {
		if rule.Start != nil {
			start = rule.Start
		}
		if rule.Due != nil {
			due = rule.Due
		}
	}
	status := "待完成"
	if done {
		status = "已完成"
	}
	if model.Meta(t)["archived"] == true {
		status = "已归档"
	}
	var states struct {
		Items []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Category string `json:"category"`
		} `json:"items"`
	}
	if err := w.call(ctx, "GET", "/projects/"+t.ProjectID+"/task-statuses", nil, &states); err != nil {
		return nil, err
	}
	for _, item := range states.Items {
		if item.ID == t.StatusID {
			status = statusLabel(item.Name, item.Category)
			break
		}
	}
	card := TaskCard{Title: t.Title, Content: plainBlocks(t.Description), Start: start, Due: due, Priority: priorityLabel(t.Importance), Status: status, Tags: t.Tags, Source: "任务中心", Timezone: cfg.Timezone}
	if model.Meta(t)["source"] == "tasknotes" {
		card.Source = "Obsidian 任务"
	}
	if card.Tags == nil {
		card.Tags = []string{}
	}
	raw, _ := json.Marshal(card)
	return map[string]string{"task_card_version": "1", "task_card": string(raw)}, nil
}
