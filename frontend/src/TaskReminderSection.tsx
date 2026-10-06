import { useEffect,useState } from "react";
import {api,request,inputClass,buttonClass} from "./api";

export default function TaskReminderSection({projectId,taskId,canEdit=true}:{projectId:string;taskId:string;canEdit?:boolean}) {
 const base=`/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`;
 const [start,setStart]=useState("");const [due,setDue]=useState("");const [timezone,setTimezone]=useState("Asia/Shanghai");
 const [startMinutes,setStartMinutes]=useState("10");const [dueMinutes,setDueMinutes]=useState("10");
 const [enabled,setEnabled]=useState(true);const [startEnabled,setStartEnabled]=useState(true);const [dueEnabled,setDueEnabled]=useState(true);
 const [revision,setRevision]=useState(0);const [status,setStatus]=useState("正在读取提醒…");const [error,setError]=useState("");const [busy,setBusy]=useState(false);
 async function load(){const r=await api<{revision:number;rule:Record<string,unknown>|null;plan:{state:string}|null}>(`${base}/reminders`);setRevision(r.revision);setStatus(r.plan?.state??"awaiting_precise_time");if(r.rule){setStart(String(r.rule.start??""));setDue(String(r.rule.due??""));setEnabled(r.rule.enabled===true);setStartEnabled(r.rule.start_enabled===true);setDueEnabled(r.rule.due_enabled===true);setStartMinutes(String(r.rule.start_minutes??10));setDueMinutes(String(r.rule.due_minutes??10))}}
 useEffect(()=>{load().catch(e=>setError(e.message))},[projectId,taskId]);
 return <section className="rounded-xl border bg-card p-4 space-y-3"><h3 className="font-medium">PushGo 提醒</h3><p role="status" className="text-sm text-muted-foreground">{status}</p>{error&&<p role="alert" className="text-sm text-destructive">{error}</p>}
  <p className="text-sm text-muted-foreground">精确到时分的确认只用于提醒，不修改 Paca 原任务日期。TaskNotes 明确时刻可自动接入；原任务日期变化后需重新确认。</p>
  <label className="flex gap-2 text-sm"><input type="checkbox" checked={enabled} onChange={e=>setEnabled(e.target.checked)}/>启用该任务提醒</label>
  <label className="block text-sm">时区<input className={inputClass} value={timezone} onChange={e=>setTimezone(e.target.value)}/></label>
  <div className="grid gap-3 sm:grid-cols-2"><div className="space-y-2"><label className="flex gap-2 text-sm"><input type="checkbox" checked={startEnabled} onChange={e=>setStartEnabled(e.target.checked)}/>开始提醒</label><input aria-label="精确开始时间" className={inputClass} placeholder="2026-10-07T09:00 或含偏移的 ISO 时间" value={start} onChange={e=>setStart(e.target.value)}/><label className="text-sm">提前分钟<input type="number" min="0" max="1440" className={inputClass} value={startMinutes} onChange={e=>setStartMinutes(e.target.value)}/></label></div><div className="space-y-2"><label className="flex gap-2 text-sm"><input type="checkbox" checked={dueEnabled} onChange={e=>setDueEnabled(e.target.checked)}/>截止提醒</label><input aria-label="精确截止时间" className={inputClass} placeholder="2026-10-07T10:00 或含偏移的 ISO 时间" value={due} onChange={e=>setDue(e.target.value)}/><label className="text-sm">提前分钟<input type="number" min="0" max="1440" className={inputClass} value={dueMinutes} onChange={e=>setDueMinutes(e.target.value)}/></label></div></div>
  <button className={buttonClass} disabled={busy||!canEdit} onClick={async()=>{setBusy(true);setError("");try{const task=await request<{data:unknown}>(`/api/v1${base}`);await api(`${base}/reminders`,"PUT",{revision,enabled,start_enabled:startEnabled,due_enabled:dueEnabled,start,due,timezone,start_minutes:Number(startMinutes),due_minutes:Number(dueMinutes),base_task:task.data});await load()}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}}}>保存精确提醒</button>
 </section>;
}
