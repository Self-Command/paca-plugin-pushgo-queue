"""Both independently released plugins installed on one unmodified official host."""
aid='com.selfcommand.tasknotes-webhook'
amanifest=json.loads((ROOT/f'release/wasm/{aid}/plugin.json').read_text())
request('POST','/admin/plugins',{'name':aid,'version':amanifest['version'],'manifest':amanifest,'enabled':True},201)
asecret=request('POST',f'/plugins/{aid}/admin/worker-credential',{},201)['secret']
connection=request('POST',f'/plugins/{aid}/projects/{project["id"]}/connections',{'name':'Combined plugin acceptance','timezone':'Asia/Shanghai'},201)
(secret_dir/'a-worker-secret').write_text(asecret)
(secret_dir/'a-worker-secret').chmod(0o600)
image=(ROOT/'pair-image.txt').read_text().strip()
cmd('docker','run','-d','--name','paca-ci-tasknotes-worker','--network','paca-ci','--user','0:0','-v',f'{secret_dir}:/run/test-secrets:ro','-e','PACA_API_URL=http://paca-ci-api:8080','-e','DATABASE_URL=postgres://postgres:ci-only-password@paca-ci-db:5432/paca?sslmode=disable','-e','PACA_API_KEY_FILE=/run/test-secrets/api-key','-e','WORKER_SECRET_FILE=/run/test-secrets/a-worker-secret',image)
threading.Thread(target=server.serve_forever,daemon=True).start()
worker_process=subprocess.Popen(['/tmp/pushgo-worker'],env=worker_env,stdout=open(verification/'pair-worker.log','w'),stderr=subprocess.STDOUT)
target=instant(615)
event={'event':'task.created','timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'vault':{'name':'Pair fixture','path':'/ci/pair'},'data':{'task':{'id':'Tasks/Pair.md','path':'Tasks/Pair.md','title':'两个独立插件联合提醒','status':'open','priority':'high','scheduled':target,'tags':['pair-ci'],'dateModified':datetime.datetime.now(datetime.timezone.utc).isoformat()}}}
raw=json.dumps(event,ensure_ascii=False).encode()
headers={'Content-Type':'application/json','X-TaskNotes-Event':'task.created','X-TaskNotes-Delivery-ID':'pair-create','X-TaskNotes-Signature':hmac.new(connection['secret'].encode(),raw,hashlib.sha256).hexdigest()}
with opener.open(urllib.request.Request(base+f'/plugins/{aid}/receive/{connection["id"]}',data=raw,method='POST',headers=headers)) as r: assert r.status==202
before=len(accepted)
for _ in range(120):
    sources=request('GET',f'/plugins/{aid}/projects/{project["id"]}/connections/{connection["id"]}/sources')['items']
    if sources and sources[0]['task_id']:
        imported=sources[0]['task_id']
        rows=[j for j in queue() if j['task_id']==imported]
        if any(j['state']=='gateway_accepted' for j in rows):break
    time.sleep(0.5)
else:raise AssertionError(f'pair import/queue failed: sources={sources}; jobs={queue()}')
assert len(accepted)==before+1
assert next(b for b in accepted.values() if b['title']=='任务即将开始：两个独立插件联合提醒')['severity']=='high'
request('GET',f'/projects/{project["id"]}/tasks/{imported}')
worker_process.terminate();worker_process.wait(timeout=10)
cmd('docker','stop','paca-ci-tasknotes-worker')
server.shutdown()
(verification/'pair-report.json').write_text(json.dumps({'official_paca':'0.18.6','tasknotes_release':'v0.1.0-dev.19','tasknotes_source':json.loads((ROOT/'pair-info.json').read_text())['source_sha'],'independent_plugins_joint_import_and_delivery':True},indent=2))
