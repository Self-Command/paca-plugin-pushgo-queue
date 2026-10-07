import hashlib, hmac, http.cookiejar, json, os, pathlib, secrets, subprocess, time, urllib.request, urllib.error

ROOT=pathlib.Path(__file__).resolve().parent.parent
source_manifest=json.loads((ROOT/'plugin.json').read_text())
manifest=json.loads((ROOT/f'release/wasm/{source_manifest["id"]}/plugin.json').read_text())
plugin_id=manifest['id']
exec((ROOT/'scripts/upgrade-prepare.py').read_text(),globals())
password=secrets.token_urlsafe(24)
new_password=secrets.token_urlsafe(24)

def cmd(*args):
    subprocess.run(args,check=True,stdout=subprocess.DEVNULL)

cmd('docker','network','create','paca-ci')
cmd('docker','run','-d','--name','paca-ci-db','--network','paca-ci','-e','POSTGRES_PASSWORD=ci-only-password','-e','POSTGRES_DB=paca','postgres:16-alpine')
cmd('docker','run','-d','--name','paca-ci-cache','--network','paca-ci','valkey/valkey:8-alpine')
for _ in range(50):
    if subprocess.run(['docker','exec','paca-ci-db','pg_isready','-U','postgres'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0: break
    time.sleep(1)
env={'DATABASE_URL':'postgres://postgres:ci-only-password@paca-ci-db:5432/paca?sslmode=disable','REDIS_URL':'redis://paca-ci-cache:6379','JWT_SECRET':secrets.token_hex(32),'ADMIN_USERNAME':'admin','ADMIN_PASSWORD':password,'ENCRYPTION_KEY':secrets.token_hex(32),'PUBLIC_URL':'http://localhost:18080','COOKIE_SECURE':'false','PLUGINS_WASM_DIR':'/plugins/wasm','PLUGINS_FRONTEND_DIR':'/plugins/frontend','STORAGE_PROVIDER':'s3','STORAGE_ENDPOINT':'http://paca-ci-storage:9000','STORAGE_ACCESS_KEY_ID':'ci-access-key','STORAGE_SECRET_ACCESS_KEY':'ci-secret-key','AI_AGENT_INTERNAL_KEY':secrets.token_hex(32),'STORAGE_BUCKET':'paca','STORAGE_REGION':'us-east-1'}
args=['docker','run','-d','--name','paca-ci-api','--network','paca-ci','--network-alias','api','-p','127.0.0.1:18080:8080','-v',f'{ROOT}/release:/plugins']
for k,v in env.items(): args+=['-e',f'{k}={v}']
args+=['pacaai/paca-api@sha256:42b36fcb167f39bf07c04464b6d71ea49b1f7a8a745a2e9391623e76c5a17ad9']
cmd(*args)
jar=http.cookiejar.CookieJar()
opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
base='http://localhost:18080/api/v1'
def request(method,path,data=None,expected=200,headers=None):
    body=json.dumps(data).encode() if data is not None else None
    req=urllib.request.Request(base+path,data=body,method=method,headers={'Content-Type':'application/json',**(headers or {})})
    try:
        with opener.open(req,timeout=20) as r: status,payload=r.status,r.read()
    except urllib.error.HTTPError as e: status,payload=e.code,e.read()
    if status!=expected: raise RuntimeError(f'{method} {path}: {status}: {payload.decode()}')
    return json.loads(payload) if payload else {}
for _ in range(90):
    try:
        request('POST','/auth/login',{'username':'admin','password':password})
        break
    except (OSError,RuntimeError): time.sleep(2)
else: raise RuntimeError('official API did not become ready')
request('PATCH','/users/me/password',{'current_password':password,'new_password':new_password},204)
request('POST','/auth/login',{'username':'admin','password':new_password})
installed=request('POST','/admin/plugins',{'name':plugin_id,'version':legacy_manifest['version'],'manifest':legacy_manifest,'enabled':True},201)['data']
assert request('GET',f'/plugins/{plugin_id}/health')['schema_version']==2
worker_secret=request('POST',f'/plugins/{plugin_id}/admin/worker-credential',{},201)['secret']
request('PATCH',f'/admin/plugins/{installed['id']}',{'enabled':False})
cmd('docker','cp',str(current_package)+ '/.',f'paca-ci-api:/plugins/wasm/{plugin_id}/')
request('PATCH',f'/admin/plugins/{installed['id']}',{'manifest':manifest,'version':manifest['version'],'enabled':True})
health=request('GET',f'/plugins/{plugin_id}/health')
assert health['schema_version']==3 and health['id']==plugin_id
stamp=str(int(time.time()))
nonce=secrets.token_hex(24)
signature=hmac.new(worker_secret.encode(),f'GET\n/worker/control\n{stamp}\n{nonce}'.encode(),hashlib.sha256).hexdigest()
worker_headers={'X-Worker-Timestamp':stamp,'X-Worker-Nonce':nonce,'X-Worker-Signature':signature}
control=request('GET',f'/plugins/{plugin_id}/worker/control',headers=worker_headers)
assert control['enabled'] and control['schema_version']==3
request('GET',f'/plugins/{plugin_id}/worker/control',expected=409,headers=worker_headers)
request('GET',f'/plugins/{plugin_id}/worker/control',expected=401)
project=request('POST','/projects',{'name':'Plugin baseline','task_id_prefix':'CI'},201)['data']
request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/status')
task=request('POST',f'/projects/{project["id"]}/tasks',{'title':'Official task preserved'},201)['data']
request('PATCH',f'/projects/{project["id"]}/tasks/{task["id"]}',{'title':'Official update preserved'})
request('PATCH',f'/admin/plugins/{installed["id"]}',{'enabled':False})
request('GET',f'/plugins/{plugin_id}/health',expected=404)
request('GET',f'/projects/{project["id"]}/tasks/{task["id"]}')
request('PATCH',f'/admin/plugins/{installed["id"]}',{'enabled':True})
request('GET',f'/plugins/{plugin_id}/health')
cmd('docker','restart','paca-ci-api')
time.sleep(5)
request('GET',f'/plugins/{plugin_id}/health')
exec((ROOT/'scripts/queue-integration.py').read_text(),globals())
exec((ROOT/'scripts/pair-integration.py').read_text(),globals())
verification=ROOT/'verification'
verification.mkdir(parents=True,exist_ok=True)
(ROOT/'ci.Caddyfile').write_text(':80 {\n handle /api/* {\n  reverse_proxy paca-ci-api:8080\n }\n handle_path /plugins/* {\n  root * /var/www/plugins\n  file_server\n }\n handle_path /plugins-mcp/* {\n  root * /var/www/plugins-mcp\n  file_server\n }\n handle {\n  reverse_proxy paca-ci-web:3000\n }\n}\n')
cmd('docker','run','-d','--name','paca-ci-web','--network','paca-ci','pacaai/paca-web@sha256:c65dc2fa6384be8bbafdda9a220d525d54c730f63f2a0e0b4c167c7bb9452995')
cmd('docker','run','-d','--name','paca-ci-caddy','--network','paca-ci','-p','127.0.0.1:18081:80','-v',f'{ROOT}/ci.Caddyfile:/etc/caddy/Caddyfile:ro','-v',f'{ROOT}/release/frontend:/var/www/plugins:ro','-v',f'{ROOT}/release/mcp:/var/www/plugins-mcp:ro','caddy:2-alpine')
for _ in range(40):
    try:
        with urllib.request.urlopen('http://127.0.0.1:18081/api/healthz',timeout=2) as ready:
            if ready.status==200: break
    except OSError: time.sleep(0.5)
else: raise RuntimeError('Caddy did not become ready')

outsider_password=secrets.token_urlsafe(24)
outsider_new_password=secrets.token_urlsafe(24)
outsider_user=request('POST','/admin/users',{'username':'ci-outsider','password':outsider_password,'full_name':'CI permission outsider'},201)['data']
outsider_opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
def outsider(method,path,data):
    with outsider_opener.open(urllib.request.Request(base+path,data=json.dumps(data).encode(),method=method,headers={'Content-Type':'application/json'})) as r:
        body=r.read()
        return json.loads(body) if body else {}
outsider('POST','/auth/login',{'username':'ci-outsider','password':outsider_password})
outsider('PATCH','/users/me/password',{'current_password':outsider_password,'new_password':outsider_new_password})
outsider('POST','/auth/login',{'username':'ci-outsider','password':outsider_new_password})
outsider_key=outsider('POST','/users/me/api-keys',{'name':'MCP outsider'})['data']['key']
(secret_dir/'mcp-context.json').write_text(json.dumps({'base_url':'http://127.0.0.1:18080','gateway_url':'http://127.0.0.1:18081','api_key':api_key,'outsider_key':outsider_key,'project_id':project['id'],'task_id':native['id']}))
(secret_dir/'mcp-context.json').chmod(0o600)
cmd('bun','scripts/mcp-smoke.ts')
reader_role=request('POST',f'/projects/{project["id"]}/roles',{'role_name':'CI core-only reader','permissions':{'tasks.read':True}},201)['data']
request('POST',f'/projects/{project["id"]}/members',{'user_id':outsider_user['id'],'project_role_id':reader_role['id']},201)
with urllib.request.urlopen(urllib.request.Request(base+f'/projects/{project["id"]}/tasks/{native["id"]}',headers={'X-API-Key':outsider_key}),timeout=20) as r: assert r.status==200
try:
    urllib.request.urlopen(urllib.request.Request(base+f'/plugins/{plugin_id}/projects/{project["id"]}/tasks/{native["id"]}/reminders',headers={'X-API-Key':outsider_key}),timeout=20)
    raise AssertionError('Core-only reader escaped plugin permission')
except urllib.error.HTTPError as error: assert error.code==403
mcp_report=json.loads((verification/'mcp-report.json').read_text())
mcp_report.update({'historical_package_upgrade':True,'upgrade_source':legacy_source,'worker_credential_preserved':True,'manifest_reload_preserves_business_data':True,'core_reader_kept':True,'custom_plugin_permission_enforced':True})
(verification/'mcp-report.json').write_text(json.dumps(mcp_report,indent=2))
from playwright.sync_api import sync_playwright
with sync_playwright() as pw:
    browser=pw.chromium.launch()
    context=browser.new_context(viewport={'width':1440,'height':1000})
    response=context.request.post('http://127.0.0.1:18081/api/v1/auth/login',data={'username':'admin','password':new_password})
    assert response.ok, 'browser login unsuccessful'
    page=context.new_page()
    page.goto(f'http://127.0.0.1:18081/projects/{project["id"]}/settings/',wait_until='domcontentloaded')
    page.get_by_role('button',name=manifest['displayName'],exact=True).last.click(timeout=45000)
    page.get_by_role('status').filter(has_text='已连接宿主').wait_for(timeout=30000)
    page.screenshot(path=str(verification/'plugin-settings.png'),full_page=True)
    page.goto(f'http://127.0.0.1:18081/projects/{project["id"]}/tasks/{native["id"]}',wait_until='domcontentloaded')
    page.get_by_role('button',name='保存精确提醒',exact=True).wait_for(timeout=30000)
    page.get_by_label('精确开始时间',exact=True).fill(instant(86400))
    with page.expect_response(lambda r:r.request.method=='PUT' and r.url.endswith('/reminders')) as saved_response:
        page.get_by_role('button',name='保存精确提醒',exact=True).click()
    assert saved_response.value.status==202,'Reminder UI save failed'
    page.get_by_role('status').filter(has_text='等待队列更新').wait_for(timeout=10000)
    assert request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/tasks/{native["id"]}/reminders')['rule']['timezone']=='Asia/Shanghai'
    page.screenshot(path=str(verification/'task-reminder.png'),full_page=True)
    browser.close()
retained_jobs={j['id']:(j['op_id'],j['state']) for j in queue()}
request('PATCH',f'/admin/plugins/{installed["id"]}',{'manifest':manifest,'version':manifest['version'],'enabled':True})
assert request('GET',f'/plugins/{plugin_id}/health')['schema_version']==3
assert {j['id']:(j['op_id'],j['state']) for j in queue()}==retained_jobs,'Manifest reload changed queue history'
request('DELETE',f'/admin/plugins/{installed["id"]}',expected=204)
request('GET',f'/plugins/{plugin_id}/health',expected=404)
request('GET',f'/projects/{project["id"]}/tasks/{task["id"]}')
request('GET','/plugins/com.selfcommand.tasknotes-webhook/health')
report={'official_paca':'0.18.6','plugin':plugin_id,'migration':True,'wasm':True,'worker_hmac':True,'nonce_replay_rejected':True,'frontend_host':True,'task_crud':True,'disable_enable':True,'restart':True}
report.update({'manifest_reload':True,'uninstall_preserves_core_and_other_plugin':True,'task_time_zone_persisted':True,'task_panel_save':True})
(verification/'host-report.json').write_text(json.dumps(report,indent=2))
print(json.dumps({'official_paca':'0.18.6','plugin':plugin_id,'migration':True,'wasm':True,'task_crud':True,'disable_enable':True,'restart':True}))
