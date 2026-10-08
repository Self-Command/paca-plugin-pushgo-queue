"""B obtains a real restricted action from the independently released C plugin."""
cid='com.selfcommand.task-checkin'
cmanifest=json.loads((ROOT/f'release/wasm/{cid}/plugin.json').read_text())
request('POST','/admin/plugins',{'name':cid,'version':cmanifest['version'],'manifest':cmanifest,'enabled':True},201)
csecret=request('POST',f'/plugins/{cid}/admin/worker-credential',{},201)['secret']
states=request('GET',f'/projects/{project["id"]}/task-statuses')['data']['items']
progress_status=next(s['id'] for s in states if s['category']=='inprogress')
done_status=next(s['id'] for s in states if s['category']=='done')
archive_status=request('POST',f'/projects/{project["id"]}/task-statuses',{'name':'CI 归档','category':'done','position':99},201)['data']['id']
cp=f'/plugins/{cid}/projects/{project["id"]}'
request('PUT',cp+'/settings',{'revision':0,'config':{'enabled':True,'timezone':'Asia/Shanghai','start_minutes':10,'due_minutes':10,'retention_days':90,'progress_status':progress_status,'done_status':done_status,'archive_status':archive_status}})
action_secret=secrets.token_hex(32)
for name,value in [('checkin-worker-secret',csecret),('checkin-action-secret',action_secret),('checkin-grant-secret',secrets.token_hex(32)),('checkin-storage-access','ci-access-key'),('checkin-storage-secret','ci-secret-key')]:
    (secret_dir/name).write_text(value);(secret_dir/name).chmod(0o600)
cmd('docker','run','-d','--name','paca-ci-checkin-storage','--network','paca-ci','-e','RUSTFS_ACCESS_KEY=ci-access-key','-e','RUSTFS_SECRET_KEY=ci-secret-key','-e','RUSTFS_VOLUMES=/data/rustfs0','--tmpfs','/data/rustfs0:mode=777','rustfs/rustfs:1.0.0-rc.6')
image=(ROOT/'checkin-image.txt').read_text().strip()
args=['docker','run','-d','--name','paca-ci-checkin-worker','--network','paca-ci','--user','0:0','-p','127.0.0.1:19092:8090','-v',f'{secret_dir}:/run/test-secrets:ro']
values={'PACA_API_URL':'http://paca-ci-api:8080','DATABASE_URL':'postgres://postgres:ci-only-password@paca-ci-db:5432/paca?sslmode=disable','PUBLIC_URL':'https://task.example.org','PACA_API_KEY_FILE':'/run/test-secrets/api-key','WORKER_SECRET_FILE':'/run/test-secrets/checkin-worker-secret','GRANT_SECRET_FILE':'/run/test-secrets/checkin-grant-secret','ACTION_SECRET_FILE':'/run/test-secrets/checkin-action-secret','STORAGE_ACCESS_KEY_FILE':'/run/test-secrets/checkin-storage-access','STORAGE_SECRET_KEY_FILE':'/run/test-secrets/checkin-storage-secret','CHECKIN_S3_ENDPOINT':'http://paca-ci-checkin-storage:9000','CHECKIN_BUCKET':'checkin-private'}
for k,v in values.items():args+=['-e',f'{k}={v}']
cmd(*args,image)
for _ in range(40):
    try:
        with urllib.request.urlopen('http://127.0.0.1:19092/healthz',timeout=3) as r:
            if r.status==200:break
    except OSError:time.sleep(1)
else:raise AssertionError('Independent C worker unavailable')
threading.Thread(target=server.serve_forever,daemon=True).start()
current=request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings')
request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',{**current['config'],'revision':current['revision'],'checkin_enabled':True})
worker_env.update({'CHECKIN_WORKER_URL':'http://127.0.0.1:19092','CHECKIN_SERVICE_SECRET_FILE':str(secret_dir/'checkin-action-secret')})
worker_process=subprocess.Popen(['/tmp/pushgo-worker'],env=worker_env,stdout=open(verification/'checkin-pair-worker.log','w'),stderr=subprocess.STDOUT)
t=core_task('任务照片与可靠推送',75)
request('PATCH',f'/projects/{project["id"]}/tasks/{t["id"]}',{'tags':['学习'],'description':[{'type':'paragraph','content':[{'type':'text','text':'阅读第五章，整理三个要点。'}],'children':[]}]})
request('PUT',cp+f'/tasks/{t["id"]}/checkin',{'revision':0,'config':{'enabled':True,'start':instant(180),'due':instant(240)}})
# Verify C eligibility before waiting on the durable B worker.
probe_req=urllib.request.Request('http://127.0.0.1:19092/internal/v1/task',data=json.dumps({'project_id':project['id'],'task_id':t['id']}).encode(),method='POST',headers={'Content-Type':'application/json','Authorization':'Bearer '+action_secret})
for probe_attempt in range(12):
    try:
        with urllib.request.urlopen(probe_req,timeout=20) as response:assert json.load(response)['enabled']
        break
    except urllib.error.HTTPError as error:
        print('C initial task response:',error.code,error.read().decode()[:500])
        if error.code != 503 or probe_attempt == 11:raise
        time.sleep(1)

before=len(accepted);lost_response=False
try:wait_state(t['id'],'gateway_accepted')
except Exception:
    (verification/'checkin-pair-host.log').write_text(subprocess.check_output(['docker','logs','paca-ci-api','--tail','120'],stderr=subprocess.STDOUT).decode())
    probe={ 'b_health':request('GET',f'/plugins/{plugin_id}/health'),'c_health':request('GET',f'/plugins/{cid}/health'),'settings':request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings'),'jobs':queue() }
    candidates=request('GET',f'/projects/{project["id"]}/tasks?page_size=200')['data']['items']
    probe['c_control_schema']=subprocess.check_output(['docker','exec','paca-ci-db','psql','-U','postgres','-d','paca','-Atc','SELECT count(*),bool_and(enabled) FROM plugin_data_com_selfcommand_task_checkin.worker_settings'],text=True).strip()
    probe['task_plans']=[]
    for candidate in candidates:
        req=urllib.request.Request('http://127.0.0.1:19092/internal/v1/task',data=json.dumps({'project_id':project['id'],'task_id':candidate['id']}).encode(),method='POST',headers={'Content-Type':'application/json','Authorization':'Bearer '+action_secret})
        try:
            with urllib.request.urlopen(req,timeout=15) as response:status,payload=response.status,response.read()
        except urllib.error.HTTPError as error:status,payload=error.code,error.read()
        probe['task_plans'].append({'title':candidate['title'],'status':status,'response':json.loads(payload)})
    (verification/'checkin-pair-failure.json').write_text(json.dumps(probe,indent=2))
    logs=subprocess.check_output(['docker','logs','paca-ci-checkin-worker','--tail','100'],stderr=subprocess.STDOUT,text=True)
    (verification/'checkin-c-worker.log').write_text(logs)
    raise
bodies=[v for v in accepted.values() if v['title'].endswith(t['title'])]
assert bodies and len(accepted)>=before+1
for body in bodies:
    metadata=body['metadata'];assert metadata['action_version']=='1' and metadata['action_kind']=='web'
    assert metadata['action_url'].startswith('https://task.example.org/checkin/') and '#token=' in metadata['action_url']
    card=json.loads(metadata['task_card']);assert card['content']=='阅读第五章，整理三个要点。' and card['priority']=='高' and card['tags']==['学习']
# Gateway handler already checks that all response-loss retries preserve the full snapshot.
# The same canonical operation drives B and C; no per-plugin task time copy.
import uuid
frozen_view=task_times(t['id'])
frozen_update={'op_id':str(uuid.uuid4()),'kind':'update','task_id':t['id'],'base_version':frozen_view['version'],'times':{**frozen_view['times'],'start':{'precision':'instant','value':instant(3600)},'due':{'precision':'instant','value':instant(7200)}}}
request('POST',operation_root,frozen_update,202)
frozen_result=operation_result(frozen_update['op_id'])
assert frozen_result['state']=='conflict' and frozen_result['result']['status_code']==409,frozen_result
canonical={'op_id':str(uuid.uuid4()),'kind':'create','title':'统一时间与打卡联调','times':{'start':{'precision':'instant','value':instant(3600)},'due':{'precision':'instant','value':instant(7200)},'timezone':'Asia/Shanghai','start_minutes':10,'due_minutes':10}}
request('POST',operation_root,canonical,202);canonical_result=operation_result(canonical['op_id']);assert canonical_result['state']=='applied',canonical_result
canonical_id=canonical_result['task_id'];old_jobs=wait_state(canonical_id,'scheduled');old_ops={j['op_id'] for j in old_jobs if j['state']=='scheduled'}
def canonical_plan():
    req=urllib.request.Request('http://127.0.0.1:19092/internal/v1/task',data=json.dumps({'project_id':project['id'],'task_id':canonical_id}).encode(),method='POST',headers={'Content-Type':'application/json','Authorization':'Bearer '+action_secret})
    with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
plan_before=canonical_plan();assert not plan_before['frozen']
view=task_times(canonical_id)
canonical_update={'op_id':str(uuid.uuid4()),'kind':'update','task_id':canonical_id,'base_version':view['version'],'times':{**canonical['times'],'start':{'precision':'instant','value':instant(5400)}}}
request('POST',operation_root,canonical_update,202);rescheduled=operation_result(canonical_update['op_id']);assert rescheduled['state']=='applied',rescheduled
plan_after=canonical_plan();assert plan_after['instance_id']!=plan_before['instance_id'] and plan_after['revision']>plan_before['revision']
expected_time=datetime.datetime.fromisoformat(canonical_update['times']['start']['value'])
assert datetime.datetime.fromisoformat(plan_after['start'].replace('Z','+00:00'))==expected_time
for _ in range(80):
    replacement_jobs=[j for j in queue() if j['task_id']==canonical_id]
    if all(j['state']=='superseded' for j in replacement_jobs if j['op_id'] in old_ops):break
    time.sleep(.25)
assert all(j['state']=='superseded' for j in replacement_jobs if j['op_id'] in old_ops),replacement_jobs
request('POST',cp+f'/tasks/{canonical_id}/cancel',{})
request('POST',cp+f'/tasks/{t["id"]}/cancel',{})
wait_state(t['id'],'superseded') if any(j['state'] in ('scheduled','retry_wait','sending') for j in queue() if j['task_id']==t['id']) else None
worker_process.terminate();worker_process.wait(timeout=10)
current=request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings')
request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',{**current['config'],'revision':current['revision'],'checkin_enabled':False})
server.shutdown();cmd('docker','stop','paca-ci-checkin-worker')
(verification/'checkin-pair-report.json').write_text(json.dumps({'c_source':json.loads((ROOT/'checkin-info.json').read_text())['source_sha'],'independent_plugin_action':True,'full_chinese_card':True,'stable_action_after_response_loss':True,'precise_c_window_used':True,'no_schema_sharing':True,'canonical_times_match_checkin':True,'before_open_reschedule_replaces_instance_and_jobs':True,'after_open_time_change_conflict':True},indent=2))
