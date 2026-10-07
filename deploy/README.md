# Official Paca with independent plugins

This directory keeps the official v0.18.6 Compose and Caddy files unchanged. Their source SHA and file hashes
are recorded in `official/upstream.json`. The overlay adds two independent workers and binds the gateway
only to `127.0.0.1:18788`. No Paca core fork or server compilation is needed.
The small Caddy wrapper trusts the existing Nginx forwarding headers on the private network and imports
the unchanged official routes. Only trusted containers belong on this network.

Download **deployment.tar.gz from a verified release**. It includes the Action-produced
`official-images.json` and `compose.images.yaml`, pinning the official stack and infrastructure by digest.
The Git source directory alone does not contain those generated locks. Docker Compose >= 2.24.4 is required.

## Staged installation

1. Extract this directory into `/opt/paca-task-platform`, preserving `official/`.
2. Create protected `secrets/`, `backups/` and `plugins/{wasm,frontend,mcp,skills}` directories.
3. Copy `env.example` to `.env`, generate real random values locally, and chmod `.env` to 0600.
   Fill the five official image variables from `official-images.json`. Fill each worker image from its own
   release's `image-digest.txt`. Install each matching `plugin-install.tar.gz` into `plugins/` by copying its
   `wasm`, `frontend` and `mcp` contents. Never mix a WASM source SHA with a different worker source SHA.
4. Start the upstream services without worker profiles:

```sh
docker compose --env-file .env -f official/docker-compose.yaml -f compose.plugins.yaml -f compose.images.yaml up -d
```

5. Complete the initial Paca administrator password change. Register both included manifests via the official
   plugin administration API/UI; the API loads the compiled WASM and applies each plugin's own migrations.
6. Create separate worker service accounts and personal API keys, restricted to intended projects.
   A needs `tasks.read`, `tasks.write`, `tasks.delete`; B needs `tasks.read`. Give neither a global admin role.
   Create dedicated PostgreSQL login roles granting USAGE on each respective schema, CRUD on its tables
   and USAGE/SELECT on its sequences. Grant default privileges for new tables/sequences created by the Paca
   migration owner, so upgrades do not break workers. They do not need core-schema table privileges.
7. Generate each worker HMAC credential via that plugin's `/admin/worker-credential` endpoint. Save the
   corresponding API key and worker secret files. B also needs the existing runtime Gateway token and
   the same AES encryption key as Paca's API. Secret files must be owned by UID 65532 and mode 0400;
   the worker images run as this unprivileged UID. Keep the enclosing server directory root-only.
8. Set the two restricted worker PostgreSQL URLs in `.env`, then enable the worker profiles:

```sh
docker compose --env-file .env -f official/docker-compose.yaml -f compose.plugins.yaml -f compose.images.yaml --profile tasknotes --profile pushgo up -d
```

9. Configure A's project connection and six official TaskNotes webhook subscriptions. Configure B's copied
   channel ID, channel display name and password. The Gateway token is never a browser or project setting.
10. Verify the stack on localhost before changing production Nginx. The example `nginx-task.conf` keeps
    the shared certificate, HTTP ACME/redirect, websocket upgrade and unbuffered streaming. Define the
    documented `map` and webhook `limit_req_zone` once in the existing `http` context. The webhook limit
    is 50 requests/second with a burst of 100; tune it for bulk imports and actual trusted proxy IP handling.
    Reuse actual existing ACME paths and certificate
    names, run `nginx -t` against the active master's configuration, then reload that specific master.
    `push.spacedo.org`, Gateway WSS and Cabinet remain separate and unchanged.

To use only one plugin, omit the other manifest, package, worker profile and project settings. The plugins
do not query each other's database schema. The overlay is deployment orchestration, not a merged plugin.

## Backup, cutover and rollback

Before cutover, save `docker inspect` mount inventories, exact old images, Compose configuration and active
Nginx files in a protected archive. Do not print Docker environment variables or upload those archives to
GitHub. Stop only the old check-in service briefly to archive its SQLite files, photos and exclusive mounts
consistently; restart it while Paca is validated. Transfer the recoverable archive and SHA256 to protected
local storage and verify the local hash before deleting any server data.

Validate HTTPS, login, original tasks/views, attachments, realtime, both plugin settings and a controlled
test reminder after switching Nginx. A real AI conversation additionally needs a valid model configuration;
checking routes and MCP tools alone is not a real model conversation. On any cutover failure restore the
old Nginx file and check-in service, while retaining Paca's data for investigation.

After acceptance, remove only the old check-in container and verified exclusive data/configuration.
Resolve and inspect every target path and mount first. Remove its service and check-in-only secrets from
the old deployment configuration. Preserve Gateway/WSS, Cabinet and shared certificates/renewal hooks.
Never run `docker compose down -v`, a global prune, or recursively delete the shared old deployment root.

Paca's daily PostgreSQL dump includes both plugin schemas. Also back up object storage, encryption keys,
runtime secret files, exact release manifests and image locks. For a consistent object-storage archive,
pause writers and stop RustFS during the archive. Test restoration in a separate stack before relying on
the backup; database dumps alone do not restore attachments or encrypted credentials.

## Upgrade and pause

Back up first. Disable the target plugin/worker, replace its complete matching package and image digest,
let the official API apply its included migrations, then re-enable. A disabled/uninstalled/mismatched host
causes its worker to pause external operations. Removing a plugin does not implicitly delete its data;
use a separate explicit data-retention decision. Existing TaskNotes events that never reached the receiver
cannot be reconstructed. Gateway-accepted notifications cannot be recalled.
