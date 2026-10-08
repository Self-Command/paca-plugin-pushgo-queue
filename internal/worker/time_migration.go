package worker
import("context";"encoding/json";"errors";"time";"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model")
func (w *Worker) migrateRuleTimes(ctx context.Context,s settings,t model.Task,r *model.Rule)(model.Task,*model.Rule,error){
 if r==nil||r.Start==nil&&r.Due==nil{return t,r,nil}
 if r.BaseFingerprint!=model.Fingerprint(t){return t,r,errors.New("原提醒时间与任务版本不一致，请核对后重新确认。")}
 if s.Config.CheckinEnabled {
 var state struct{Frozen bool `json:"frozen"`;Start *time.Time `json:"legacy_start"`;Due *time.Time `json:"legacy_due"`}
 if err:=w.checkinCall(ctx,"/internal/v1/times/freeze",map[string]string{"project_id":s.Project,"task_id":t.ID},&state);err!=nil{return t,r,err}
 if state.Frozen{return t,r,errors.New("已有打卡窗口已开放，原提醒时间需要核对。")}
 if state.Start!=nil&&(r.Start==nil||!state.Start.Equal(*r.Start))||state.Due!=nil&&(r.Due==nil||!state.Due.Equal(*r.Due)){return t,r,errors.New("提醒与打卡专属时间不同，请核对后重新确认。")}
 }
 times:=model.TimesOf(t)
 for _,entry:=range []struct{key string;value *time.Time;dst *model.TimeValue}{{"start",r.Start,&times.Start},{"due",r.Due,&times.Due}}{
 if entry.value==nil{continue};precise:=model.Precise(t,entry.key,t.Start);if entry.key=="due"{precise=model.Precise(t,entry.key,t.Due)}
 if precise!=nil&&!precise.Equal(*entry.value){return t,r,errors.New("原提醒时间与统一任务时间不同，请核对后重新确认。")}
 *entry.dst=model.TimeValue{Precision:"instant",Value:entry.value.Format(time.RFC3339Nano)}
 }
 if r.StartMinutes!=nil{times.StartMinutes=*r.StartMinutes};if r.DueMinutes!=nil{times.DueMinutes=*r.DueMinutes}
 patch,err:=model.TimePatch(t,times);if err!=nil{return t,r,err}
 var fresh model.Task
 path:="/projects/"+s.Project+"/tasks/"+t.ID
 if err=w.call(ctx,"GET",path,nil,&fresh);err!=nil{return t,r,err}
 if model.TimeVersion(fresh)!=model.TimeVersion(t){return t,r,errors.New("任务正在被修改，请稍后核对。")}
 if err=w.call(ctx,"PATCH",path,patch,&fresh);err!=nil{return t,r,err}
 var verified model.Task;if err=w.call(ctx,"GET",path,nil,&verified);err!=nil{return t,r,err}
 if !operationMatches(verified,model.TaskOperation{Times:&times}){return t,r,errors.New("统一时间尚未确认，请核对实际结果。")}
 old,_:=json.Marshal(r);r.Start=nil;r.Due=nil;r.StartMinutes=nil;r.DueMinutes=nil;r.BaseFingerprint=model.Fingerprint(verified);raw,_:=json.Marshal(r)
 tag,err:=w.DB.Exec(ctx,"UPDATE task_rules SET config=$3::jsonb,updated_at=clock_timestamp() WHERE project_id=$1 AND task_id=$2 AND config=$4::jsonb",s.Project,t.ID,string(raw),string(old));if err!=nil{return t,r,err};if tag.RowsAffected()!=1{return t,r,errors.New("提醒设置发生变化，请刷新后重新确认。")}
 return verified,r,nil
}
