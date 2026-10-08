import {useEffect,useState} from "react";
import {api} from "./api";
import {Card,CardHeader,CardTitle,CardContent} from "./components/ui/card";
import {Button} from "./components/ui/button";
import {Switch} from "./components/ui/switch";
import TaskTimesForm,{emptyTimes,type TaskTimes} from "./TaskTimesForm";
import {operationKey,waitOperation} from "./task-operations";
import "./theme.css";

type TimeView={state:string;version:string;times:TaskTimes;frozen:boolean|null;error?:string};
const states:Record<string,string>={active:"已安排提醒",awaiting_precise_time:"待确认准确时间",awaiting_reconciliation:"等待安排更新",confirmation_stale:"时间需要重新确认",time_conflict:"时间存在冲突，请核对",cancelled:"已取消提醒",identity_conflict:"任务关联需要核对",checkin_unavailable:"打卡安排暂时不可用"};
export default function TaskReminderSection({projectId,taskId,canEdit=true}:{projectId:string;taskId:string;canEdit?:boolean}){
 const base=`/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`;
 const storage=operationKey(projectId)+":"+taskId;
 const [times,setTimes]=useState(emptyTimes);
 const [version,setVersion]=useState("");const [frozen,setFrozen]=useState(false);
 const [enabled,setEnabled]=useState(true);const [startEnabled,setStartEnabled]=useState(true);const [dueEnabled,setDueEnabled]=useState(true);
 const [revision,setRevision]=useState(0);const [status,setStatus]=useState("");const [error,setError]=useState("");const [busy,setBusy]=useState(false);
 const [pending,setPending]=useState(()=>localStorage.getItem(storage)||"");
 async function load(){
  const rule=await api<{revision:number;rule:{enabled:boolean;start_enabled:boolean;due_enabled:boolean}|null;plan:{state:string}|null}>(`${base}/reminders`);
  let view:TimeView|undefined;
  for(let n=0;n<40;n++){view=await api<TimeView>(`${base}/times`);if(view.state==="ready")break;await new Promise(resolve=>setTimeout(resolve,500));}
  if(!view||view.state!=="ready")throw new Error("任务时间仍在读取，请稍后刷新。");
  setTimes(view.times);setVersion(view.version);setFrozen(view.frozen===true);setRevision(rule.revision);
  setEnabled(rule.rule?.enabled??true);setStartEnabled(rule.rule?.start_enabled??true);setDueEnabled(rule.rule?.due_enabled??true);
  setStatus(rule.plan?.state??"awaiting_precise_time");if(view.error)setError(view.error);
 }
 useEffect(()=>{setVersion("");setError("");setPending(localStorage.getItem(storage)||"");load().catch(e=>setError(e.message))},[projectId,taskId]);
 useEffect(()=>{const timer=setInterval(()=>api<{plan:{state:string}|null}>(`${base}/reminders`).then(r=>setStatus(r.plan?.state??"awaiting_precise_time")).catch(()=>{}),5000);return()=>clearInterval(timer)},[projectId,taskId]);
 async function save(){
  const resuming=!!pending;
  setBusy(true);setError("");
  try{
   let op=pending;let body:unknown;
   if(!op){op=crypto.randomUUID();body={op_id:op,kind:"update",task_id:taskId,base_version:version,...(frozen?{}:{times}),reminders:{revision,enabled,start_enabled:startEnabled,due_enabled:dueEnabled}};localStorage.setItem(storage+":body",JSON.stringify(body));localStorage.setItem(storage,op);setPending(op)}
   else{const saved=localStorage.getItem(storage+":body");if(saved)body=JSON.parse(saved)}
   if(body)await api(`/projects/${projectId}/task-operations`,"POST",body);
   await waitOperation(projectId,op);localStorage.removeItem(storage);localStorage.removeItem(storage+":body");setPending("");await load();
  }catch(e){if(typeof e==="object"&&e!==null&&(("operationState" in e&&["failed","conflict"].includes(String(e.operationState)))||(!resuming&&"status" in e&&[400,401,403,409,422].includes(Number(e.status))))){localStorage.removeItem(storage);localStorage.removeItem(storage+":body");setPending("");await load().catch(()=>{})}setError(e instanceof Error?e.message:"任务时间暂时无法保存。")}finally{setBusy(false)}
 }
 return <div className="checkin-ui"><Card className="gap-3 py-4"><CardHeader className="py-0"><CardTitle>任务时间与提醒</CardTitle></CardHeader><CardContent className="grid gap-4 max-h-[65vh] overflow-y-auto">
  <p role="status" className="text-sm text-muted-foreground">{states[status]??"正在读取安排…"}</p>
  <p className="text-sm text-muted-foreground">任务同步、推送和打卡使用以下时间。仅填写日期不会安排定时提醒。</p>
  {frozen&&<p className="text-sm text-muted-foreground">打卡窗口已开放，时间已冻结。请取消原实例后再改期。</p>}
  <TaskTimesForm value={times} onChange={setTimes} disabled={!canEdit||busy||!!pending||!version||frozen}/>
  <fieldset disabled={!canEdit||busy||!!pending||!version} className="grid gap-3"><label className="flex items-center gap-2 text-sm"><Switch checked={enabled} onCheckedChange={setEnabled}/>启用该任务提醒</label><label className="flex items-center gap-2 text-sm"><Switch checked={startEnabled} onCheckedChange={setStartEnabled}/>开始提醒</label><label className="flex items-center gap-2 text-sm"><Switch checked={dueEnabled} onCheckedChange={setDueEnabled}/>结束提醒</label></fieldset>
  {error&&<p role="alert" className="text-sm text-destructive">{error}</p>}
  <div className="flex flex-wrap gap-2"><Button disabled={!canEdit||busy||!version} onClick={save}>{busy?"正在保存…":pending?"查询保存结果":"保存时间与提醒"}</Button><Button variant="outline" disabled={busy||!!pending} onClick={()=>{setError("");load().catch(e=>setError(e.message))}}>刷新安排</Button></div>
 </CardContent></Card></div>;
}
