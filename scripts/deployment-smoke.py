"""Actions only: pin official images and exercise the complete upstream Compose."""
import hashlib,http.cookiejar,json,os,pathlib,secrets,shutil,subprocess,time,urllib.request
import websocket
ROOT=pathlib.Path(__file__).resolve().parent.parent
deploy=ROOT/'deploy'
verification=ROOT/'verification'
verification.mkdir(exist_ok=True)
upstream=json.loads((deploy/'official/upstream.json').read_text())
for name,digest in upstream['files'].items():
    assert hashlib.sha256((deploy/name).read_bytes()).hexdigest()==digest,'Upstream deployment was changed'
fixed=json.loads((deploy/'official/images.lock.json').read_text())
assert fixed['source_sha']==upstream['source_sha'] and fixed['paca_version']==upstream['version']
refs=fixed['images']
images={}
for name,ref in refs.items():
    digest=subprocess.check_output(['docker','buildx','imagetools','inspect',ref,'--format','{{.Manifest.Digest}}'],text=True).strip()
    assert digest.startswith('sha256:') and len(digest)==71
    assert digest==ref.split('@')[1],'Pinned image identity changed'
    images[name]=ref
(verification/'official-images.json').write_text(json.dumps({'paca_version':'v0.18.6','source_sha':upstream['source_sha'],'images':images},indent=2))
lock='services:\n'+''.join(f'  {service}:\n    image: {images[key]}\n' for service,key in [('postgres','postgres'),('db-backup','postgres'),('valkey','valkey'),('rustfs','rustfs'),('rustfs-init','rustfs'),('gateway','gateway')])
(verification/'compose.images.yaml').write_text(lock)
stage=ROOT/'ci-full-stack'
stage.mkdir(mode=0o700,exist_ok=True)
shutil.copytree(deploy/'official',stage/'official',dirs_exist_ok=True)
shutil.copyfile(deploy/'caddy-wrapper.Caddyfile',stage/'caddy-wrapper.Caddyfile')
for name in ['wasm','frontend','mcp']:
    shutil.copytree(ROOT/f'release/{name}',stage/f'plugins/{name}',dirs_exist_ok=True)
(stage/'plugins/skills').mkdir(parents=True,exist_ok=True)
(stage/'secrets').mkdir(mode=0o700,exist_ok=True)
config={**{key:value for key,value in images.items() if key.isupper()},'PACA_DEPLOY_ROOT':str(stage),'PUBLIC_URL':'http://127.0.0.1:18788','STORAGE_PUBLIC_URL':'http://127.0.0.1:18788/storage','CORS_ORIGINS':'http://127.0.0.1:18788','COOKIE_SECURE':'false','TZ':'Asia/Shanghai','POSTGRES_USER':'paca','POSTGRES_DB':'paca','ADMIN_USERNAME':'admin','BACKUP_DIR':str(stage/'backups'),'WORKER_CONCURRENCY':'1','TASKNOTES_DATABASE_URL':'postgres://unused:unused@postgres/paca','PUSHGO_DATABASE_URL':'postgres://unused:unused@postgres/paca','TASKNOTES_WORKER_IMAGE':images['PACA_API_IMAGE'],'PUSHGO_WORKER_IMAGE':images['PACA_API_IMAGE']}
for key in ['POSTGRES_PASSWORD','ADMIN_PASSWORD','JWT_SECRET','ENCRYPTION_KEY','INTERNAL_API_KEY','AGENT_API_KEY','STORAGE_ACCESS_KEY_ID','STORAGE_SECRET_ACCESS_KEY']: config[key]=secrets.token_hex(32)
envfile=stage/'runtime.env'
envfile.write_text(''.join(f'{key}={value}\n' for key,value in config.items()))
envfile.chmod(0o600)
certs=stage/'test-certificates'
certs.mkdir(mode=0o700,exist_ok=True)
subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(certs/'privkey.pem'),'-out',str(certs/'fullchain.pem'),'-days','1','-subj','/CN=task.spacedo.org'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
(stage/'nginx.conf').write_text('events {}\nhttp {\n map $http_upgrade $paca_connection_upgrade { default upgrade; "" close; }\n limit_req_zone $binary_remote_addr zone=paca_tasknotes_hook:10m rate=50r/s;\n include /etc/nginx/task.conf;\n}\n')
subprocess.run(['docker','run','--rm','-v',f'{stage}/nginx.conf:/etc/nginx/nginx.conf:ro','-v',f'{deploy}/nginx-task.conf:/etc/nginx/task.conf:ro','-v',f'{certs}:/etc/letsencrypt/live/tasknotes-pushgo-spacedo:ro','nginx:1.18','nginx','-t'],check=True)
compose=['docker','compose','--project-name','paca-ci-full','--env-file',str(envfile),'-f',str(stage/'official/docker-compose.yaml'),'-f',str(deploy/'compose.plugins.yaml'),'-f',str(verification/'compose.images.yaml')]
rendered=json.loads(subprocess.check_output(compose+['config','--format','json'],text=True))
ports=[p for s in rendered['services'].values() for p in s.get('ports',[])]
assert len(ports)==1 and ports[0]['host_ip']=='127.0.0.1' and str(ports[0]['published'])=='18788'
assert rendered['services']['agent-runner']['environment']['WORKER_CONCURRENCY']=='1'
subprocess.run(compose+['up','-d'],check=True)
jar=http.cookiejar.CookieJar()
opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
base='http://127.0.0.1:18788/api/v1'
def request(method,path,data=None):
    with opener.open(urllib.request.Request(base+path,data=None if data is None else json.dumps(data).encode(),method=method,headers={'Content-Type':'application/json'}),timeout=20) as r:
        raw=r.read()
        return json.loads(raw) if raw else {}
for _ in range(150):
    try:
        request('POST','/auth/login',{'username':'admin','password':config['ADMIN_PASSWORD']})
        break
    except OSError: time.sleep(2)
else: raise RuntimeError('Full official stack did not become ready')
new_password=secrets.token_hex(32)
request('PATCH','/users/me/password',{'current_password':config['ADMIN_PASSWORD'],'new_password':new_password})
request('POST','/auth/login',{'username':'admin','password':new_password})
project=request('POST','/projects',{'name':'Full deployment verification','task_id_prefix':'FULL'})['data']
task=request('POST',f'/projects/{project["id"]}/tasks',{'title':'Original task and attachment'})['data']
payload=b'Paca original attachment survives plugin deployment.\n'
attachments=f'/projects/{project["id"]}/tasks/{task["id"]}/attachments'
upload=request('POST',attachments+'/initiate-upload',{'file_name':'verification.txt','content_type':'text/plain','file_size':len(payload)})['data']
with urllib.request.urlopen(urllib.request.Request(upload['upload_url'],data=payload,method='PUT',headers={'Content-Type':'text/plain'}),timeout=20) as r: assert r.status in (200,204)
attached=request('POST',attachments+'/complete-upload',{'file_id':upload['file_id']})['data']
url=request('GET',attachments+f'/{attached["id"]}/download-url')['data']['url']
with urllib.request.urlopen(url,timeout=20) as r: assert r.read()==payload
cookie='; '.join(f'{c.name}={c.value}' for c in jar)
ws=websocket.create_connection('ws://127.0.0.1:18788/ws/socket.io/?EIO=4&transport=websocket',cookie=cookie,origin='http://127.0.0.1:18788',timeout=15)
assert ws.recv().startswith('0')
ws.send('40')
assert ws.recv().startswith('40'),'Realtime authentication failed'
ws.close()
request('GET',f'/projects/{project["id"]}/agents')
request('GET',f'/projects/{project["id"]}/conversations')
source_manifest=json.loads((ROOT/'plugin.json').read_text())
manifest=json.loads((ROOT/f'release/wasm/{source_manifest["id"]}/plugin.json').read_text())
request('POST','/admin/plugins',{'name':manifest['id'],'version':manifest['version'],'manifest':manifest,'enabled':True})
assert request('GET',f'/plugins/{manifest["id"]}/health')['schema_version']==5
(verification/'deployment-report.json').write_text(json.dumps({'unchanged_upstream':upstream['source_sha'],'official_images_pinned':True,'nginx_118_syntax':True,'loopback_only':True,'official_task_crud':True,'original_attachment_upload_download':True,'realtime_websocket_auth':True,'ai_routes_preserved':True,'agent_concurrency':1,'actual_model_chat':'not_executed_requires_model_configuration','plugins_on_full_stack':True},indent=2))
subprocess.run(compose+['down'],check=True)
