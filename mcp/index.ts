// Self-contained Paca MCP entry: every call retains the MCP caller's personal API key.
type Context={pluginId:string;baseURL:string;apiKey:string};
const properties={project_id:{type:"string",description:"Paca project UUID"},task_id:{type:"string",description:"Paca task UUID"}};
const tools=[
 {name:"pushgo_get_reminders",description:"Query precise reminders, confirmation status and revision for a Paca task.",inputSchema:{type:"object",properties,required:["project_id","task_id"]}},
 {name:"pushgo_get_delivery_status",description:"Query reminder jobs. Gateway accepted is not proof of phone delivery.",inputSchema:{type:"object",properties,required:["project_id","task_id"]}},
 {name:"pushgo_set_reminders",description:"Confirm exact start/due reminder times for a task. Supply ISO times with UTC offsets, or local times and IANA timezone. Date-only values are invalid. Does not change the core task dates. Task date changes invalidate this confirmation.",inputSchema:{type:"object",properties:{...properties,start:{type:"string"},due:{type:"string"},timezone:{type:"string",default:"Asia/Shanghai"},start_minutes:{type:"integer",minimum:0,maximum:1440},due_minutes:{type:"integer",minimum:0,maximum:1440},enabled:{type:"boolean",default:true},start_enabled:{type:"boolean",default:true},due_enabled:{type:"boolean",default:true},revision:{type:"integer",minimum:0,description:"Base rule revision from pushgo_get_reminders"}},required:["project_id","task_id","revision"]}},
];
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
async function call(context:Context,path:string,method="GET",body?:unknown){
 const response=await fetch(`${context.baseURL.replace(/\/$/,"")}/api/v1${path}`,{method,headers:{"X-API-Key":context.apiKey,"Content-Type":"application/json"},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(15000),redirect:"error"});
 if(!response.ok)throw new Error(`Paca rejected this caller's request: HTTP ${response.status}`);return response.json();
}
export default {
 tools,
 async handleToolCall(name:string,args:Record<string,unknown>,context:Context){
  try{
   if(!uuid.test(String(args.project_id))||!uuid.test(String(args.task_id)))throw new Error("Valid project and task UUIDs required");
   const core=`/projects/${args.project_id}/tasks/${args.task_id}`;
   // Verify caller's task access, even when querying a plugin-owned job.
   const current=await call(context,core);
   const plugin=`/plugins/${context.pluginId}${core}`;
   let value;
   if(name==="pushgo_get_reminders")value=await call(context,`${plugin}/reminders`);
   else if(name==="pushgo_get_delivery_status")value=await call(context,`${plugin}/deliveries`);
   else if(name==="pushgo_set_reminders"){
    const body={enabled:args.enabled??true,start_enabled:args.start_enabled??true,due_enabled:args.due_enabled??true,timezone:args.timezone??"Asia/Shanghai",start:args.start??"",due:args.due??"",revision:args.revision,base_task:current.data,...(args.start_minutes===undefined?{}:{start_minutes:args.start_minutes}),...(args.due_minutes===undefined?{}:{due_minutes:args.due_minutes})};
    value=await call(context,`${plugin}/reminders`,"PUT",body);
   }else throw new Error("Unknown PushGo tool");
   return {content:[{type:"text",text:JSON.stringify(value)}]};
  }catch(error){return {isError:true,content:[{type:"text",text:error instanceof Error?error.message:"PushGo request failed"}]}}
 }
};
