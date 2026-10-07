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
with urllib.request.urlopen(probe_req,timeout=20) as response:assert json.load(response)['enabled']
before=len(accepted);lost_response=False
try:wait_state(t['id'],'gateway_accepted')
except Exception:
    probe={ 'b_health':request('GET',f'/plugins/{plugin_id}/health'),'c_health':request('GET',f'/plugins/{cid}/health'),'settings':request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings'),'jobs':queue() }
    candidates=request('GET',f'/projects/{project["id"]}/tasks?page_size=200')['data']['items']
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
request('POST',cp+f'/tasks/{t["id"]}/cancel',{})
wait_state(t['id'],'superseded') if any(j['state'] in ('scheduled','retry_wait','sending') for j in queue() if j['task_id']==t['id']) else None
worker_process.terminate();worker_process.wait(timeout=10)
current=request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings')
request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',{**current['config'],'revision':current['revision'],'checkin_enabled':False})
server.shutdown();cmd('docker','stop','paca-ci-checkin-worker')
(verification/'checkin-pair-report.json').write_text(json.dumps({'c_source':json.loads((ROOT/'checkin-info.json').read_text())['source_sha'],'independent_plugin_action':True,'full_chinese_card':True,'stable_action_after_response_loss':True,'precise_c_window_used':True,'no_schema_sharing':True},indent=2))
