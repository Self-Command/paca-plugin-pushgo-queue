"""Pin a verified independent TaskNotes release for combined acceptance."""
import hashlib, io, json, pathlib, tarfile, urllib.request

root=pathlib.Path(__file__).resolve().parent.parent
release='https://github.com/Self-Command/paca-plugin-tasknotes-webhook/releases/download/v0.1.0-dev.19/'
def get(name):
    with urllib.request.urlopen(release+name,timeout=60) as r: return r.read()
checksums=get('checksums.txt').decode()
package=get('plugin-install.tar.gz')
expected=next(line.split()[0] for line in checksums.splitlines() if line.endswith('plugin-install.tar.gz'))
assert hashlib.sha256(package).hexdigest()==expected
info=json.loads(get('build-info.json'))
assert info['source_sha'].startswith('449154e')
with tarfile.open(fileobj=io.BytesIO(package),mode='r:gz') as archive:
    archive.extractall(root/'release',filter='data')
(root/'pair-image.txt').write_bytes(get('image-digest.txt'))
(root/'pair-info.json').write_text(json.dumps(info))
