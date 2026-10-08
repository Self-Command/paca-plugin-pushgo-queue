"""Actions only: durable official task writes and canonical-time invariants."""
import uuid,urllib.parse
from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
fault_state={'lost_create':False,'deny_check_once':False,'writes':0}
class TaskFaultProxy(BaseHTTPRequestHandler):
    def log_message(self,*args):pass
    def forward(self):
        body=self.rfile.read(int(self.headers.get('Content-Length',0))) or None
        if self.path.endswith('/task-authorization') and fault_state['deny_check_once']:
            fault_state['deny_check_once']=False;self.send_response(503);self.end_headers();self.wfile.write(b'{}');return
        req=urllib.request.Request('http://localhost:18080'+self.path,data=body,method=self.command,headers={k:v for k,v in self.headers.items() if k.lower() not in ('host','content-length','connection')})
        try:
            with urllib.request.urlopen(req,timeout=20) as reply:status,payload=reply.status,reply.read()
        except urllib.error.HTTPError as error:status,payload=error.code,error.read()
        if self.command=='POST' and self.path.endswith('/tasks') and json.loads(body or b'{}').get('title')=='创建响应丢失验收':
            fault_state['writes']+=1
            if status==201 and not fault_state['lost_create']:
                fault_state['lost_create']=True;fault_state['deny_check_once']=True;self.close_connection=True;return
        self.send_response(status);self.end_headers();self.wfile.write(payload)
    do_GET=forward;do_POST=forward;do_PATCH=forward
operation_proxy=ThreadingHTTPServer(('127.0.0.1',18183),TaskFaultProxy);threading.Thread(target=operation_proxy.serve_forever,daemon=True).start()
operation_env={**worker_env,'PACA_API_URL':'http://127.0.0.1:18183'}
worker_process=subprocess.Popen(['/tmp/pushgo-worker'],env=operation_env,stdout=open(verification/'task-operations-worker.log','w'),stderr=subprocess.STDOUT)
operation_root=f'/plugins/{plugin_id}/projects/{project["id"]}/task-operations'
def operation_result(op):
    for _ in range(160):
        r=request('GET',operation_root+'/'+urllib.parse.quote(op,safe=''))
        if r['state'] in ['applied','conflict','failed','uncertain']:return r
        time.sleep(.25)
    raise AssertionError('operation remained pending')
def task_times(task):
    for _ in range(80):
        req=urllib.request.Request(base+f'/plugins/{plugin_id}/projects/{project["id"]}/tasks/{task}/times')
        with opener.open(req,timeout=20) as reply:r=json.load(reply)
        if r.get('state')=='ready':return r
        time.sleep(.25)
    raise AssertionError('task time cache remained pending')
try:
    body={'op_id':'task:'+str(uuid.uuid4()),'kind':'create','title':'统一时间 API 任务','content':'任务内容\n第二段','importance':75,'tags':['测试'],'times':{'start':{'precision':'instant','value':instant(86400)},'due':{'precision':'instant','value':instant(90000)},'timezone':'Asia/Shanghai','start_minutes':10,'due_minutes':10}}
    request('POST',operation_root,body,202);request('POST',operation_root,body,202)
    result=operation_result(body['op_id']);assert result['state']=='applied',result
    created_task=request('GET',f'/projects/{project["id"]}/tasks/{result["task_id"]}')['data']
    assert created_task['custom_fields']['_integration_state_v1']['start_precision']=='instant'
    request('POST',operation_root,{**body,'title':'不同内容'},409)
    listed=request('GET',f'/projects/{project["id"]}/tasks?page_size=200')['data']['items']
    assert sum(t['custom_fields'].get('_pushgo_operation_v1',{}).get('op_id')==body['op_id'] for t in listed)==1
    view=task_times(result['task_id']);assert view['times']['start']['precision']=='instant'
    update={'op_id':str(uuid.uuid4()),'kind':'update','task_id':result['task_id'],'base_version':view['version'],'times':{**body['times'],'start':{'precision':'day','value':'2026-12-01'},'due':{'precision':'none','value':''}}}
    request('POST',operation_root,update,202);changed=operation_result(update['op_id']);assert changed['state']=='applied',changed
    stale_body={**update,'op_id':str(uuid.uuid4())};request('POST',operation_root,stale_body,409)
    after=request('GET',f'/projects/{project["id"]}/tasks/{result["task_id"]}')['data'];assert after['custom_fields']['_integration_state_v1']['start_precision']=='day'
    assert not after['custom_fields']['_integration_state_v1']['start_instant']
    lost={**body,'op_id':str(uuid.uuid4()),'title':'创建响应丢失验收'}
    request('POST',operation_root,lost,202);lost_result=operation_result(lost['op_id']);assert lost_result['state']=='applied',lost_result
    assert fault_state['writes']==1 and fault_state['lost_create'] and not fault_state['deny_check_once'],'unknown create was repeated after authorization retry'
    # A core date edit cannot retain a stale instant.
    exact={'op_id':str(uuid.uuid4()),'kind':'create','title':'官方日期改动','times':body['times']}
    request('POST',operation_root,exact,202);exact_result=operation_result(exact['op_id']);assert exact_result['state']=='applied',exact_result
    request('PATCH',f'/projects/{project["id"]}/tasks/{exact_result["task_id"]}',{'start_date':'2026-12-15T00:00:00Z'})
    for _ in range(80):
        view=task_times(exact_result['task_id'])
        if view['times']['start']['precision']=='day':break
        time.sleep(.25)
    assert view['times']['start']['precision']=='day'
    (verification/'task-operations-report.json').write_text(json.dumps({'create_with_content_and_time':True,'stable_operation_repeated_once':True,'operation_payload_conflict_409':True,'base_version_conflict':True,'date_only_no_midnight':True,'official_date_edit_discards_unconfirmed_time':True,'no_client_snapshot_authorization':True,'lost_create_then_auth_outage_no_duplicate':True},indent=2))
finally:
    worker_process.terminate();worker_process.wait(timeout=10);operation_proxy.shutdown()
