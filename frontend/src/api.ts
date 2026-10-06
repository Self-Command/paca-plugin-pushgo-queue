export const pluginID="com.selfcommand.pushgo-queue";
export const inputClass="w-full rounded-md border border-input bg-background px-3 py-2 text-sm";
export const buttonClass="rounded-md border border-input bg-background px-3 py-2 text-sm hover:bg-accent disabled:opacity-50";
export async function request<T>(url:string,method="GET",body?:unknown):Promise<T> {
 const r=await fetch(url,{method,credentials:"include",headers:{"Content-Type":"application/json"},body:body===undefined?undefined:JSON.stringify(body)});
 const data=await r.json();if(!r.ok)throw new Error(data.error?.message??data.message??`HTTP ${r.status}`);return data;
}
export function api<T>(path:string,method="GET",body?:unknown):Promise<T> {return request(`/api/v1/plugins/${pluginID}${path}`,method,body)}
export type Config={enabled:boolean;gateway_url:string;channel_id:string;channel_name:string;timezone:string;start_minutes:number;due_minutes:number;created_push:boolean;priority_map:Record<string,string>};
export type Job={id:number;task_id:string;kind:string;fire_at:string;expires_at:string;state:string;attempts:number;error:string;op_id:string};
