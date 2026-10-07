"""Pin a verified independent TaskNotes release for combined acceptance."""
import hashlib, io, json, pathlib, tarfile, urllib.request,urllib.error,time

root=pathlib.Path(__file__).resolve().parent.parent
release='https://github.com/Self-Command/paca-plugin-tasknotes-webhook/releases/download/v0.1.0-dev.50/'
def get(name):
    for attempt in range(90):
        try:
            with urllib.request.urlopen(release+name,timeout=60) as r: return r.read()
        except urllib.error.HTTPError as error:
            if error.code not in (404,429,502,503,504) or attempt==89: raise
            time.sleep(10)
checksums=get('checksums.txt').decode()
package=get('plugin-install.tar.gz')
expected=next(line.split()[0] for line in checksums.splitlines() if line.endswith('plugin-install.tar.gz'))
assert hashlib.sha256(package).hexdigest()==expected
info=json.loads(get('build-info.json'))
assert info['source_sha']=='b82b60b89378447d4a56d82df2cbc4a12a54a4bb'
info['release']='v0.1.0-dev.50'
with tarfile.open(fileobj=io.BytesIO(package),mode='r:gz') as archive:
    archive.extractall(root/'release',filter='data')
(root/'pair-image.txt').write_bytes(get('image-digest.txt'))
(root/'pair-info.json').write_text(json.dumps(info))

# C remains a separately built, checksum-verified official plugin package.
release='https://github.com/Self-Command/paca-plugin-task-checkin/releases/download/v0.1.0-dev.24/'
checksums=get('checksums.txt').decode();package=get('plugin-install.tar.gz')
expected=next(line.split()[0] for line in checksums.splitlines() if line.endswith('plugin-install.tar.gz'))
assert hashlib.sha256(package).hexdigest()==expected
checkin_info=json.loads(get('build-info.json'))
assert checkin_info['source_sha']=='73026ed43e1a868a2e010300aedfcf6074880672'
with tarfile.open(fileobj=io.BytesIO(package),mode='r:gz') as archive:archive.extractall(root/'release',filter='data')
(root/'checkin-image.txt').write_bytes(get('image-digest.txt'))
(root/'checkin-info.json').write_text(json.dumps(checkin_info))
