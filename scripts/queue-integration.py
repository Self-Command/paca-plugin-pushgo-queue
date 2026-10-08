"""Queue acceptance against official Paca and a TLS fake Gateway; Actions only."""
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
import datetime, ssl, threading

verification=ROOT/'verification'
verification.mkdir(exist_ok=True)
secret_dir=ROOT/'ci-secrets'
secret_dir.mkdir(mode=0o700,exist_ok=True)
gateway_token=secrets.token_hex(24)
api_key=request('POST','/users/me/api-keys',{'name':'Queue CI worker'},201)['data']['key']
for name,value in [('api-key',api_key),('worker-secret',worker_secret),('gateway-token',gateway_token),('encryption-key',env['ENCRYPTION_KEY'])]:
    (secret_dir/name).write_text(value)
    (secret_dir/name).chmod(0o600)
cmd('openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(secret_dir/'tls.key'),'-out',str(secret_dir/'tls.pem'),'-days','1','-subj','/CN=localhost','-addext','subjectAltName=IP:127.0.0.1,DNS:localhost')
accepted={}
submissions=[]
lost_response=False
transient_requests=[]
lease_received=threading.Event()
lease_release=threading.Event()
class Gateway(BaseHTTPRequestHandler):
    def log_message(self,*args): pass
    def do_POST(self):
        global lost_response
        body=json.loads(self.rfile.read(int(self.headers.get('Content-Length',0))))
        assert self.path=='/message'
        assert self.headers.get('Authorization')=='Bearer '+gateway_token
        assert body['channel_id']=='ci-channel' and body['password']=='ci-channel-password'
        assert 'message_id' not in body and len(body['op_id'])<=128
        assert body['ttl']>int(time.time()*1000)
        stable={k:v for k,v in body.items() if k not in ('password',)}
        if body['title'].endswith('Transient HTTP failures'):
            transient_requests.append((time.monotonic(),stable))
            if len(transient_requests)<=2:
                self.send_response(429 if len(transient_requests)==1 else 503)
                self.send_header('Retry-After','2')
                self.end_headers()
                self.wfile.write(b'{"error":"ci temporary failure"}')
                return
        if body['op_id'] in accepted: assert accepted[body['op_id']]==stable,'operation payload changed during retry'
        accepted[body['op_id']]=stable
        submissions.append(body)
        if body['title'].endswith('Crash during accepted submit') and not lease_received.is_set():
            lease_received.set()
            lease_release.wait(timeout=90)
            return
        if not lost_response:
            lost_response=True
            self.connection.shutdown(2)
            self.connection.close()
            return
        self.send_response(200)
        self.send_header('Content-Type','application/json')
        self.end_headers()
        self.wfile.write(json.dumps({'success':True,'data':{'op_id':body['op_id'],'message_id':'ci-message-'+body['op_id']}}).encode())
server=ThreadingHTTPServer(('127.0.0.1',19090),Gateway)
tls=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
tls.load_cert_chain(secret_dir/'tls.pem',secret_dir/'tls.key')
server.socket=tls.wrap_socket(server.socket,server_side=True)
threading.Thread(target=server.serve_forever,daemon=True).start()
cfg={'enabled':True,'gateway_url':'https://127.0.0.1:19090','channel_id':'ci-channel','channel_name':'CI','password':'ci-channel-password','timezone':'Asia/Shanghai','start_minutes':10,'due_minutes':10,'created_push':False,'revision':0}
request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',cfg)
settings=request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings')
assert 'password' not in json.dumps(settings)
cmd('docker','run','-d','--name','paca-ci-db-forward','--network','paca-ci','-p','127.0.0.1:15432:5432','alpine/socat','tcp-listen:5432,fork,reuseaddr','tcp-connect:paca-ci-db:5432')
worker_env={**os.environ,'PACA_API_URL':'http://127.0.0.1:18080','DATABASE_URL':'postgres://postgres:ci-only-password@127.0.0.1:15432/paca?sslmode=disable','PACA_API_KEY_FILE':str(secret_dir/'api-key'),'WORKER_SECRET_FILE':str(secret_dir/'worker-secret'),'GATEWAY_TOKEN_FILE':str(secret_dir/'gateway-token'),'ENCRYPTION_KEY_FILE':str(secret_dir/'encryption-key'),'PUBLIC_URL':'https://task.example.org','PUSHGO_CI_ALLOW_LOOPBACK':'true','SSL_CERT_FILE':str(secret_dir/'tls.pem')}
log=open(verification/'queue-worker.log','w')
worker_process=subprocess.Popen(['/tmp/pushgo-worker'],env=worker_env,stdout=log,stderr=log)

def core_task(title,importance=35):
    return request('POST',f'/projects/{project["id"]}/tasks',{'title':title,'importance':importance},201)['data']
def configure(t,start,revision=0,enabled=True):
    current=request('GET',f'/projects/{project["id"]}/tasks/{t["id"]}')['data']
    return request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/tasks/{t["id"]}/reminders',{'base_task':current,'revision':revision,'enabled':enabled,'start':start,'due':'','timezone':'Asia/Shanghai','start_minutes':10},202)
def queue():
    return request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/jobs')['items']
def wait_state(task_id,state):
    for _ in range(180):
        rows=[j for j in queue() if j['task_id']==task_id]
        if any(j['state']==state for j in rows):return rows
        time.sleep(0.5)
    raise AssertionError(f'job never reached {state}: {rows}')
def instant(offset):
    return (datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=offset)).isoformat()

# Native dates remain pending until explicitly confirmed.
native=core_task('Native date requires precise confirmation')
request('PATCH',f'/projects/{project["id"]}/tasks/{native["id"]}',{'start_date':instant(86400)})
time.sleep(3)
assert not any(j['task_id']==native['id'] for j in queue())

secondary_log=open(verification/'queue-secondary-worker.log','w')
secondary_worker=subprocess.Popen(['/tmp/pushgo-worker'],env=worker_env,stdout=secondary_log,stderr=secondary_log)
four=[]
for importance,severity in [(10,'low'),(35,'normal'),(75,'high'),(150,'critical')]:
    t=core_task('Four-level '+severity,importance)
    configure(t,instant(603))
    four.append((t,severity))
for t,severity in four:
    wait_state(t['id'],'gateway_accepted')
assert {b['severity'] for b in accepted.values()}=={'low','normal','high','critical'}
assert secondary_worker.poll() is None,'Concurrent worker did not remain running'
assert len(accepted)==4 and len(submissions)==5,'Only the lost acknowledgement should require resubmission'
secondary_worker.terminate();secondary_worker.wait(timeout=10)
secondary_log.close()

# Rescheduling cancels the old future job, restart preserves its replacement.
changed=core_task('Reschedule then cancel')
configure(changed,instant(3600))
first=wait_state(changed['id'],'scheduled')
old_op=next(j['op_id'] for j in first if j['state']=='scheduled')
configure(changed,instant(7200),revision=1)
time.sleep(3)
rows=[j for j in queue() if j['task_id']==changed['id']]
assert next(j for j in rows if j['op_id']==old_op)['state']=='superseded'
assert sum(j['state']=='scheduled' for j in rows)==1
worker_process.terminate();worker_process.wait(timeout=10)
worker_process=subprocess.Popen(['/tmp/pushgo-worker'],env=worker_env,stdout=log,stderr=log)
time.sleep(3)
assert len(accepted)==4
statuses=request('GET',f'/projects/{project["id"]}/task-statuses')['data']['items']
done=next(s['id'] for s in statuses if s['category']=='done')
request('PATCH',f'/projects/{project["id"]}/tasks/{changed["id"]}',{'status_id':done})
wait_state(changed['id'],'superseded')
time.sleep(3)
assert not any(j['task_id']==changed['id'] and j['state'] in ('scheduled','retry_wait','sending') for j in queue())
expired=core_task('Expired reminders are not sent')
configure(expired,instant(-5))
wait_state(expired['id'],'expired')
assert len(accepted)==4

# Deletion, source archive/recurrence and cleared dates invalidate pending jobs.
deleted=core_task('Deleted task cancels pending reminder')
configure(deleted,instant(3600))
wait_state(deleted['id'],'scheduled')
request('DELETE',f'/projects/{project["id"]}/tasks/{deleted["id"]}')
request('GET',f'/projects/{project["id"]}/tasks/{deleted["id"]}',expected=404)
wait_state(deleted['id'],'cancelled')
for flag in ['archived','recurring']:
    t=core_task('Source '+flag+' cancels pending reminder')
    configure(t,instant(3600))
    wait_state(t['id'],'scheduled')
    request('PATCH',f'/projects/{project["id"]}/tasks/{t["id"]}',{'custom_fields':{'_integration_state_v1':{flag:True}}})
    wait_state(t['id'],'superseded')
cleared=core_task('Cleared core date invalidates confirmation')
request('PATCH',f'/projects/{project["id"]}/tasks/{cleared["id"]}',{'start_date':instant(86400)})
configure(cleared,instant(3600))
wait_state(cleared['id'],'scheduled')
request('PATCH',f'/projects/{project["id"]}/tasks/{cleared["id"]}',{'start_date':None})
wait_state(cleared['id'],'superseded')
assert not any(j['state'] in ('scheduled','retry_wait','sending') for j in queue() if j['task_id'] in (deleted['id'],cleared['id']))
assert len(accepted)==4

# Project rule and host disable both pause work; re-enable recovers the same job.
paused=core_task('Disable and recover one reminder')
configure(paused,instant(625))
wait_state(paused['id'],'scheduled')
request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',{**cfg,'enabled':False,'revision':1})
wait_state(paused['id'],'cancelled')
request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',{**cfg,'enabled':True,'revision':2})
wait_state(paused['id'],'scheduled')
request('PATCH',f'/admin/plugins/{installed["id"]}',{'enabled':False})
time.sleep(27)
assert len(accepted)==4,'disabled host allowed Gateway submit'
request('PATCH',f'/admin/plugins/{installed["id"]}',{'enabled':True})
wait_state(paused['id'],'gateway_accepted')
assert len(accepted)==5
# Both HTTP throttling and a server failure retry the same frozen operation.
transient=core_task('Transient HTTP failures')
configure(transient,instant(603))
wait_state(transient['id'],'gateway_accepted')
assert len(transient_requests)==3
assert transient_requests[0][1]==transient_requests[1][1]==transient_requests[2][1]
assert transient_requests[1][0]-transient_requests[0][0]>=2,'Retry-After was ignored'
assert len(accepted)==6

# An accepted request followed by SIGKILL recovers only after the persisted lease expires.
crashed=core_task('Crash during accepted submit')
configure(crashed,instant(603))
assert lease_received.wait(timeout=60),'Crash fixture did not receive its first submit'
worker_process.kill();worker_process.wait(timeout=10)
lease_release.set()
crash_job=next(j for j in queue() if j['task_id']==crashed['id'])
assert crash_job['state']=='sending'
assert len(accepted)==7
worker_process=subprocess.Popen(['/tmp/pushgo-worker'],env=worker_env,stdout=log,stderr=log)
wait_state(crashed['id'],'gateway_accepted')
assert len(accepted)==7,'Lease recovery created an extra Gateway message'
assert sum(b['op_id']==crash_job['op_id'] for b in submissions)==2
sql=f"SELECT generation FROM plugin_data_com_selfcommand_pushgo_queue.jobs WHERE op_id='{crash_job['op_id']}'"
generation=subprocess.check_output(['docker','exec','paca-ci-db','psql','-U','postgres','-d','paca','-Atc',sql],text=True).strip()
assert int(generation)>=2,'Expired lease was not reclaimed with a new generation'
# Regression: one logical mother announcement, no automatic-period storm.
setting=request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings')
request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',{**cfg,'revision':setting['revision'],'created_push':True,'password':''})
prior=len(accepted)
mother=request('POST',f'/projects/{project["id"]}/tasks',{'title':'每日母任务通知一次','custom_fields':{'_integration_state_v1':{'recurring':True}}},201)['data']
period_ids=[]
for day in range(31):
    date=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=day+1)).date().isoformat()
    item=request('POST',f'/projects/{project["id"]}/tasks',{'title':'自动周期 '+date,'custom_fields':{'_integration_ref_v1':'period:'+mother['id']+':'+date,'_task_sync_v1':{'recurrence_parent':mother['id'],'occurrence_date':date}}},201)['data']
    period_ids.append(item['id'])
wait_state(mother['id'],'gateway_accepted')
time.sleep(3)
assert len(accepted)==prior+1,'Automatic periods generated a notification storm'
assert not any(j['task_id'] in period_ids and j['kind']=='created' for j in queue())
for _ in range(2):
    setting=request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings')
    request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',{**cfg,'revision':setting['revision'],'created_push':True,'password':''})
    request('PATCH',f'/projects/{project["id"]}/tasks/{mother["id"]}',{'title':'母任务改名仍然一次'})
    time.sleep(2)
assert len(accepted)==prior+1,'Reconciliation replayed a logical creation'
(verification/'creation-identity-report.json').write_text(json.dumps({'one_mother_announcement':True,'thirty_one_periods_without_created_alert':True,'setting_and_update_no_replay':True}))
setting=request('GET',f'/plugins/{plugin_id}/projects/{project["id"]}/settings')
request('PUT',f'/plugins/{plugin_id}/projects/{project["id"]}/settings',{**cfg,'revision':setting['revision'],'created_push':False,'password':''})
worker_process.terminate();worker_process.wait(timeout=10)
log.close();server.shutdown()
(verification/'queue-report.json').write_text(json.dumps({'http_429_503_same_operation_retry':True,'retry_after_respected':True,'crash_recovers_expired_lease':True,'new_lease_generation':True,'concurrent_workers_no_extra_submissions':True,'delete_cancels':True,'archive_cancels':True,'recurrence_not_expanded':True,'cleared_date_invalidates_confirmation':True,'native_date_requires_confirmation':True,'four_levels':True,'absolute_ttl':True,'same_op_id_after_lost_response':True,'reschedule_supersedes':True,'restart_no_duplicate':True,'complete_cancels':True,'expired_no_delivery':True,'project_disable_enable_recovers':True,'host_disable_pauses_gateway':True},indent=2))
