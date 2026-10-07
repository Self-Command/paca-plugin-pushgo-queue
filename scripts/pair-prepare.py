"""Pin a verified independent TaskNotes release for combined acceptance."""
import hashlib, io, json, pathlib, tarfile, urllib.request,urllib.error,time

root=pathlib.Path(__file__).resolve().parent.parent
release='https://github.com/Self-Command/paca-plugin-tasknotes-webhook/releases/download/v0.1.0-dev.37/'
def get(name):
    for attempt in range(36):
        try:
            with urllib.request.urlopen(release+name,timeout=60) as r: return r.read()
        except urllib.error.HTTPError as error:
            if error.code not in (404,429,502,503,504) or attempt==35: raise
            time.sleep(10)
checksums=get('checksums.txt').decode()
package=get('plugin-install.tar.gz')
expected=next(line.split()[0] for line in checksums.splitlines() if line.endswith('plugin-install.tar.gz'))
assert hashlib.sha256(package).hexdigest()==expected
info=json.loads(get('build-info.json'))
assert info['source_sha']=='6b5e3f4ad44632ad8bce8de35b8a3d4eaef387e6'
info['release']='v0.1.0-dev.37'
with tarfile.open(fileobj=io.BytesIO(package),mode='r:gz') as archive:
    archive.extractall(root/'release',filter='data')
(root/'pair-image.txt').write_bytes(get('image-digest.txt'))
(root/'pair-info.json').write_text(json.dumps(info))

# C remains a separately built, checksum-verified official plugin package.
release='https://github.com/Self-Command/paca-plugin-task-checkin/releases/download/v0.1.0-dev.13/'
checksums=get('checksums.txt').decode();package=get('plugin-install.tar.gz')
expected=next(line.split()[0] for line in checksums.splitlines() if line.endswith('plugin-install.tar.gz'))
assert hashlib.sha256(package).hexdigest()==expected
checkin_info=json.loads(get('build-info.json'))
assert checkin_info['source_sha']=='847d3be05164f774b61e398877ba737c002c199b'
with tarfile.open(fileobj=io.BytesIO(package),mode='r:gz') as archive:archive.extractall(root/'release',filter='data')
(root/'checkin-image.txt').write_bytes(get('image-digest.txt'))
(root/'checkin-info.json').write_text(json.dumps(checkin_info))
