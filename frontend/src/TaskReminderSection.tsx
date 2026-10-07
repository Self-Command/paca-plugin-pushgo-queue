import { useEffect,useState } from "react";
import {api,request} from "./api";
import {Card,CardHeader,CardTitle,CardContent} from "./components/ui/card";import {Button} from "./components/ui/button";import {Input} from "./components/ui/input";import {Switch} from "./components/ui/switch";import "./theme.css";
const states:Record<string,string>={active:"已安排提醒",awaiting_precise_time:"待确认准确时间",awaiting_reconciliation:"等待队列更新",confirmation_stale:"时间确认已失效",cancelled:"已取消提醒"};

function inputTime(value:unknown,timezone:string):string {
 const text=String(value??"");if(!text)return "";
 if(!/(Z|[+-]\d\d:\d\d)$/.test(text))return text.slice(0,16);
 const date=new Date(text);if(Number.isNaN(date.getTime()))return "";
 try{return new Intl.DateTimeFormat("sv-SE",{timeZone:timezone,year:"numeric",month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",hourCycle:"h23"}).format(date).replace(" ","T")}catch{return ""}
}

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
  const zone=String(r.rule?.timezone||p.config.timezone||"Asia/Shanghai");setTimezone(zone);
  setStartMinutes(String(r.rule?.start_minutes??p.config.start_minutes));
  setDueMinutes(String(r.rule?.due_minutes??p.config.due_minutes));
  if(r.rule){setStart(inputTime(r.rule.start,zone));setDue(inputTime(r.rule.due,zone));setEnabled(r.rule.enabled===true);setStartEnabled(r.rule.start_enabled===true);setDueEnabled(r.rule.due_enabled===true)}
 }
 useEffect(()=>{load().catch(e=>setError(e.message))},[projectId,taskId]);
 useEffect(()=>{const timer=setInterval(()=>api<{plan:{state:string}|null}>(`${base}/reminders`).then(r=>setStatus(r.plan?.state??"awaiting_precise_time")).catch(()=>{}),5000);return()=>clearInterval(timer)},[projectId,taskId]);
 return <div className="checkin-ui"><Card className="gap-3 py-4"><CardHeader className="py-0"><CardTitle>PushGo 提醒</CardTitle></CardHeader><CardContent className="grid gap-3 max-h-[50vh] overflow-y-auto"><p role="status" className="text-sm text-muted-foreground">{states[status]??"正在读取提醒…"}</p>{error&&<p role="alert" className="text-sm text-destructive">{error}</p>}
  <p className="text-sm text-muted-foreground">确认准确的开始和截止时间，以便按时提醒。已关联任务可沿用原安排，任务日期变化后需要重新确认。</p>
  <label className="flex gap-2 text-sm"><Switch checked={enabled} onCheckedChange={setEnabled}/>启用该任务提醒</label>
  <label className="block text-sm">时区<Input value={timezone} onChange={e=>setTimezone(e.target.value)}/></label>
  <div className="grid gap-3 sm:grid-cols-2"><div className="space-y-2"><label className="flex gap-2 text-sm"><Switch checked={startEnabled} onCheckedChange={setStartEnabled}/>开始提醒</label><label className="block space-y-2 text-sm">开始提醒时间<Input type="datetime-local" value={start} onChange={e=>setStart(e.target.value)}/></label><label className="text-sm">提前分钟<Input type="number" min="0" max="1440" value={startMinutes} onChange={e=>setStartMinutes(e.target.value)}/></label></div><div className="space-y-2"><label className="flex gap-2 text-sm"><Switch checked={dueEnabled} onCheckedChange={setDueEnabled}/>截止提醒</label><label className="block space-y-2 text-sm">截止提醒时间<Input type="datetime-local" value={due} onChange={e=>setDue(e.target.value)}/></label><label className="text-sm">提前分钟<Input type="number" min="0" max="1440" value={dueMinutes} onChange={e=>setDueMinutes(e.target.value)}/></label></div></div>
  <Button disabled={busy||!canEdit} onClick={async()=>{setBusy(true);setError("");try{const task=await request<{data:unknown}>(`/api/v1${base}`);await api(`${base}/reminders`,"PUT",{revision,enabled,start_enabled:startEnabled,due_enabled:dueEnabled,start,due,timezone,start_minutes:Number(startMinutes),due_minutes:Number(dueMinutes),base_task:task.data});await load()}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}}}>保存精确提醒</Button>
 </CardContent></Card></div>;
}
