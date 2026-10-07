export const pluginID="com.selfcommand.pushgo-queue";
export const inputClass="w-full rounded-md border border-input bg-background px-3 py-2 text-sm";
export const buttonClass="rounded-md border border-input bg-background px-3 py-2 text-sm hover:bg-accent disabled:opacity-50";
export async function request<T>(url:string,method="GET",body?:unknown):Promise<T> {
 let r:Response;try{r=await fetch(url,{method,credentials:"include",headers:{"Content-Type":"application/json"},body:body===undefined?undefined:JSON.stringify(body)});}catch{throw new Error("暂时无法连接，请稍后重试。")}
 const data=await r.json();if(!r.ok)throw new Error(r.status===409?"设置已发生变化，请刷新后重新保存。":r.status===403?"你没有修改此设置的权限。":r.status===400?"请检查频道、时间和提醒设置后重试。":"暂时无法连接，请稍后重试。");return data;
}
export function api<T>(path:string,method="GET",body?:unknown):Promise<T> {return request(`/api/v1/plugins/${pluginID}${path}`,method,body)}
export type Config={enabled:boolean;gateway_url:string;channel_id:string;channel_name:string;timezone:string;start_minutes:number;due_minutes:number;created_push:boolean;checkin_enabled:boolean;priority_map:Record<string,string>};
export type Job={id:number;task_id:string;kind:string;fire_at:string;expires_at:string;state:string;attempts:number;error:string;op_id:string;title:string};
