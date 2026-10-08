import {readFile,writeFile} from "node:fs/promises";
const input=JSON.parse(await readFile("ci-secrets/mcp-context.json","utf8"));
const sha="6791ae2c8cc1a893b0c6799cf7d24df303e8e0b0";
const {loadPlugins}=await import("/tmp/paca-official-plugin-loader.mjs");
const config={baseURL:input.base_url,gatewayURL:input.gateway_url,apiKey:input.api_key};
const registry=await loadPlugins(config);
const names=registry.getAllTools().map((t)=>t.name).sort();
if(JSON.stringify(names)!==JSON.stringify(["pushgo_create_task","pushgo_get_delivery_status","pushgo_get_reminders","pushgo_get_task_operation","pushgo_set_reminders","pushgo_set_task_times"]))throw new Error("Official loader did not register all three tools");
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
const denied=await registry.handleToolCall("pushgo_get_delivery_status",args,{...config,apiKey:input.outsider_key});
if(!denied?.isError)throw new Error("MCP escaped caller project permissions");
await writeFile("verification/mcp-report.json",JSON.stringify({official_loader_sha:sha,six_tools_registered:true,authorized_query_and_set:true,stale_revision_rejected:true,caller_permission_retained:true},null,2));
