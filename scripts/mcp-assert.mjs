import {readFile,writeFile} from "node:fs/promises";
const input=JSON.parse(await readFile("ci-secrets/mcp-context.json","utf8"));
const sha="6791ae2c8cc1a893b0c6799cf7d24df303e8e0b0";
const {loadPlugins}=await import("/tmp/paca-official-plugin-loader.mjs");
const config={baseURL:input.base_url,gatewayURL:input.gateway_url,apiKey:input.api_key};
const registry=await loadPlugins(config);
const names=registry.getAllTools().filter(t=>t.name.startsWith("pushgo_")).map((t)=>t.name).sort();
if(JSON.stringify(names)!==JSON.stringify(["pushgo_create_task","pushgo_get_delivery_status","pushgo_get_reminders","pushgo_get_task_operation","pushgo_set_reminders","pushgo_set_task_times"]))throw new Error("Official loader did not register all six tools");
const args={project_id:input.project_id,task_id:input.task_id};
for(const name of ["pushgo_get_reminders","pushgo_get_delivery_status"]){
 const result=await registry.handleToolCall(name,args,config);
 if(!result||result.isError)throw new Error(`${name} failed: ${JSON.stringify(result)}`);
}
const target=new Date(Date.now()+86400000).toISOString();
const saved=await registry.handleToolCall("pushgo_set_reminders",{...args,start:target,revision:0},config);
if(!saved||saved.isError)throw new Error(`Authorized set failed: ${JSON.stringify(saved)}`);
const stale=await registry.handleToolCall("pushgo_set_reminders",{...args,start:new Date(Date.now()+172800000).toISOString(),revision:0},config);
if(!stale?.isError)throw new Error("Stale revision was accepted");
const createArgs={project_id:input.project_id,op_id:crypto.randomUUID(),title:"AI 工具精确任务",content:"MCP 批量创建验证",times:{start:{precision:"instant",value:target},due:{precision:"none",value:""},timezone:"Asia/Shanghai",start_minutes:10,due_minutes:10}};
for(let i=0;i<3;i++){
 const body={...createArgs,op_id:crypto.randomUUID(),title:`AI 批量任务 ${i}`};
 const r=await registry.handleToolCall("pushgo_create_task",body,config);if(!r||r.isError)throw new Error(`MCP create failed: ${JSON.stringify(r)}`);
 const result=JSON.parse(r.content[0].text);if(result.state!=="applied"||!result.task_id)throw new Error("MCP create did not confirm actual task");
 const repeated=await registry.handleToolCall("pushgo_create_task",body,config);if(repeated?.isError||JSON.parse(repeated.content[0].text).task_id!==result.task_id)throw new Error("MCP repeated creation changed task");
 const deniedCreate=await registry.handleToolCall("pushgo_create_task",{...body,op_id:crypto.randomUUID()},{...config,apiKey:input.outsider_key});if(!deniedCreate?.isError)throw new Error("MCP create escaped project permissions");
}
const beforeChange=await registry.handleToolCall("pushgo_get_reminders",args,config);
const beforeView=JSON.parse(beforeChange.content[0].text).task_times;
const changedTime=new Date(Date.now()+172800000).toISOString();
const changed=await registry.handleToolCall("pushgo_set_task_times",{...args,op_id:crypto.randomUUID(),base_version:beforeView.version,times:{...beforeView.times,start:{precision:"instant",value:changedTime}}},config);
if(changed?.isError||JSON.parse(changed.content[0].text).state!=="applied")throw new Error(`MCP canonical update failed: ${JSON.stringify(changed)}`);
// A date-only create must remain date-only and never be reported as precise.
const dayBody={...createArgs,op_id:crypto.randomUUID(),title:"AI 仅日期任务",times:{...createArgs.times,start:{precision:"day",value:"2026-12-25"}}};
const dayResult=await registry.handleToolCall("pushgo_create_task",dayBody,config);if(dayResult?.isError)throw new Error(JSON.stringify(dayResult));
const dayTask=JSON.parse(dayResult.content[0].text);if(dayTask.result?.times?.start?.precision!=="day")throw new Error("Date-only AI task fabricated a precise time");
const denied=await registry.handleToolCall("pushgo_get_delivery_status",args,{...config,apiKey:input.outsider_key});
if(!denied?.isError)throw new Error("MCP escaped caller project permissions");
await writeFile("verification/mcp-report.json",JSON.stringify({official_loader_sha:sha,six_tools_registered:true,authorized_query_and_set:true,stale_revision_rejected:true,caller_permission_retained:true,batch_precise_create:true,repeated_create_once:true,create_permission_retained:true,canonical_update_tool:true,date_only_truthful:true},null,2));
