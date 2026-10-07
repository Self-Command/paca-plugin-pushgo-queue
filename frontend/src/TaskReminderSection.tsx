import { useEffect,useState } from "react";
import {api,request,inputClass,buttonClass} from "./api";
const states:Record<string,string>={active:"已安排提醒",awaiting_precise_time:"待确认准确时间",awaiting_reconciliation:"等待队列更新",confirmation_stale:"时间确认已失效",cancelled:"已取消提醒"};

export default function TaskReminderSection({projectId,taskId,canEdit=true}:{projectId:string;taskId:string;canEdit?:boolean}) {
 const base=`/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`;
 const [start,setStart]=useState("");const [due,setDue]=useState("");const [timezone,setTimezone]=useState("Asia/Shanghai");
 const [startMinutes,setStartMinutes]=useState("10");const [dueMinutes,setDueMinutes]=useState("10");
 const [enabled,setEnabled]=useState(true);const [startEnabled,setStartEnabled]=useState(true);const [dueEnabled,setDueEnabled]=useState(true);
 const [revision,setRevision]=useState(0);const [status,setStatus]=useState("正在读取提醒…");const [error,setError]=useState("");const [busy,setBusy]=useState(false);
 async function load(){
  const [r,p]=await Promise.all([
   api<{revision:number;rule:Record<string,unknown>|null;plan:{state:string}|null}>(`${base}/reminders`),
   api<{config:{timezone:string;start_minutes:number;due_minutes:number}}>(`/projects/${encodeURIComponent(projectId)}/settings`),
  ]);
  setRevision(r.revision);setStatus(r.plan?.state??"awaiting_precise_time");
  setTimezone(String(r.rule?.timezone||p.config.timezone||"Asia/Shanghai"));
  setStartMinutes(String(r.rule?.start_minutes??p.config.start_minutes));
  setDueMinutes(String(r.rule?.due_minutes??p.config.due_minutes));
  if(r.rule){setStart(String(r.rule.start??""));setDue(String(r.rule.due??""));setEnabled(r.rule.enabled===true);setStartEnabled(r.rule.start_enabled===true);setDueEnabled(r.rule.due_enabled===true)}
 }
 useEffect(()=>{load().catch(e=>setError(e.message))},[projectId,taskId]);
 useEffect(()=>{const timer=setInterval(()=>api<{plan:{state:string}|null}>(`${base}/reminders`).then(r=>setStatus(r.plan?.state??"awaiting_precise_time")).catch(()=>{}),5000);return()=>clearInterval(timer)},[projectId,taskId]);
 return <section className="rounded-xl border bg-card p-4 space-y-3"><h3 className="font-medium">PushGo 提醒</h3><p role="status" className="text-sm text-muted-foreground">{states[status]??status}</p>{error&&<p role="alert" className="text-sm text-destructive">{error}</p>}
  <p className="text-sm text-muted-foreground">精确到时分的确认只用于提醒，不修改 Paca 原任务日期。TaskNotes 明确时刻可自动接入；原任务日期变化后需重新确认。</p>
  <label className="flex gap-2 text-sm"><input type="checkbox" checked={enabled} onChange={e=>setEnabled(e.target.checked)}/>启用该任务提醒</label>
  <label className="block text-sm">时区<input className={inputClass} value={timezone} onChange={e=>setTimezone(e.target.value)}/></label>
  <div className="grid gap-3 sm:grid-cols-2"><div className="space-y-2"><label className="flex gap-2 text-sm"><input type="checkbox" checked={startEnabled} onChange={e=>setStartEnabled(e.target.checked)}/>开始提醒</label><input aria-label="精确开始时间" className={inputClass} placeholder="2026-10-07T09:00 或含偏移的 ISO 时间" value={start} onChange={e=>setStart(e.target.value)}/><label className="text-sm">提前分钟<input type="number" min="0" max="1440" className={inputClass} value={startMinutes} onChange={e=>setStartMinutes(e.target.value)}/></label></div><div className="space-y-2"><label className="flex gap-2 text-sm"><input type="checkbox" checked={dueEnabled} onChange={e=>setDueEnabled(e.target.checked)}/>截止提醒</label><input aria-label="精确截止时间" className={inputClass} placeholder="2026-10-07T10:00 或含偏移的 ISO 时间" value={due} onChange={e=>setDue(e.target.value)}/><label className="text-sm">提前分钟<input type="number" min="0" max="1440" className={inputClass} value={dueMinutes} onChange={e=>setDueMinutes(e.target.value)}/></label></div></div>
  <button className={buttonClass} disabled={busy||!canEdit} onClick={async()=>{setBusy(true);setError("");try{const task=await request<{data:unknown}>(`/api/v1${base}`);await api(`${base}/reminders`,"PUT",{revision,enabled,start_enabled:startEnabled,due_enabled:dueEnabled,start,due,timezone,start_minutes:Number(startMinutes),due_minutes:Number(dueMinutes),base_task:task.data});await load()}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}}}>保存精确提醒</button>
 </section>;
}
