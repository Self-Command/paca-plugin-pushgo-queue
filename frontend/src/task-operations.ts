import {api} from "./api";
export type OperationResult={op_id:string;state:string;task_id?:string;result?:{task_id:string};error?:string};
export async function waitOperation(project:string,op:string):Promise<OperationResult>{
 for(let n=0;n<90;n++){const result=await api<OperationResult>(`/projects/${project}/task-operations/${encodeURIComponent(op)}`);if(result.state==="applied")return result;if(["failed","conflict","uncertain"].includes(result.state))throw Object.assign(new Error(result.error||"任务结果需要核对，请刷新后重试。"),{operationState:result.state});await new Promise(resolve=>setTimeout(resolve,1000));}
 throw new Error("任务仍在处理中。请点击查询结果，不要重复创建。");
}
export const operationKey=(project:string)=>`pushgo-task-operation:${project}`;
