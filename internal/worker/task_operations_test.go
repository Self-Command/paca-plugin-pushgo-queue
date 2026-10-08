package worker
import("encoding/json";"testing";"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model")
func TestOperationVerifiesActualSavedFields(t *testing.T){
 title:="统一时间任务";times:=model.TaskTimes{Start:model.TimeValue{Precision:"instant",Value:"2026-10-10T09:00+08:00"},Due:model.TimeValue{Precision:"day",Value:"2026-10-11"},Timezone:"Asia/Shanghai",StartMinutes:10,DueMinutes:10}
 times.Start.Value="2026-10-10T09:00:00+08:00"
 patch,err:=model.TimePatch(model.Task{},times);if err!=nil{t.Fatal(err)};raw,_:=json.Marshal(patch);var task model.Task;_=json.Unmarshal(raw,&task);task.Title=title
 op:=model.TaskOperation{Title:&title,Times:&times}
 if !operationMatches(task,op){t.Fatal("valid canonical task rejected")}
 task.Title="并发修改";if operationMatches(task,op){t.Fatal("changed title accepted")};task.Title=title
 task.Custom["_integration_state_v1"].(map[string]any)["start_instant"]="2026-10-10T02:00:00Z"
 if operationMatches(task,op){t.Fatal("changed time accepted")}
}
