package worker

import "testing"

func TestTaskCardContentAndChineseEnums(t *testing.T) {
	blocks := []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"text": "完整任务正文"}}}}
	if plainBlocks(blocks) != "完整任务正文" || statusLabel("In Progress", "inprogress") != "进行中" || priorityLabel(75) != "高" {
		t.Fatal("native card lost content or exposed enum")
	}
}
