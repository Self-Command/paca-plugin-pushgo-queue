# PushGo 推送队列

An independent Apache-2.0 plugin for **official Paca v0.18.6**. It can operate without TaskNotes.
The current phase-2 candidate is undergoing Action acceptance. Earlier phase-0 releases are scaffolds.

## Behavior

- Project channel binding, encrypted channel password, default start/due lead minutes and configurable priority mapping.
- Generic precise-time confirmation in task details and three caller-scoped MCP tools.
- Native Paca date-only values remain pending until confirmed. Paca uses SQL DATE: accurate integration instants live in metadata and must match the recorded current calendar day and timezone.
- Persistent plans and jobs, transactional revision replacement, leases with owner/generation, stable logical `op_id` and absolute expiry.
- Task events prompt reconciliation; a complete official REST listing runs periodically. Incomplete listings never cancel unseen tasks.
- Re-read the core task and status immediately before submission. Completed, deleted, disabled or source-archived tasks stop future reminders.
- Retry a lost response with the same operation and saved payload. Once attempted, edits do not mutate that operation's payload.
- `gateway_accepted` means the Gateway accepted the request; it does not mean the phone displayed it.
- HTTPS Gateway with DNS validation, pinned connection addresses and redirect rejection. Runtime credentials never appear in the browser or build.

Defaults: start/due each **10 minutes**, **Asia/Shanghai**, creation-immediate push off.
Priority buckets: none → normal, low (1–19) → low, medium (20–49) → normal,
high (50–99) → high, critical (100+) → critical. Zero lead uses a five-minute send window.

## Runtime installation

Extract the verified release's `plugin-install.tar.gz`:

- `wasm/<plugin-id>` → API `PLUGINS_WASM_DIR`
- `frontend/<plugin-id>` → gateway plugin asset directory
- `mcp/<plugin-id>` → gateway MCP asset directory

Register the included manifest through Paca administration. Set the manifest's relative MCP URL
against the official gateway using `PACA_GATEWAY_URL`, so the official MCP loader can fetch it.
Keep the manifest, WASM, migrations, frontend, MCP and worker from the same verified source SHA.
The worker rejects a mismatched host source or schema, even when semantic versions match.

1. Create a restricted integration account with task/status read permission in intended projects, and create its personal API key.
2. Grant a dedicated PostgreSQL role access only to `plugin_data_com_selfcommand_pushgo_queue` and its sequences.
3. POST `{}` as an authorized administrator to `/api/v1/plugins/com.selfcommand.pushgo-queue/admin/worker-credential`.
   Save its one-time secret to a restricted runtime file.
4. Configure the exact worker image with the files below. Mount secrets read-only and readable by its nonroot UID.
5. In **PushGo 推送队列**, bind the Gateway URL, copied channel ID and password. The channel name is display-only.
6. For native Paca/API/AI tasks, open **PushGo 提醒** or use MCP to confirm exact instants. Task date changes invalidate confirmation.

```dotenv
PACA_API_URL=http://api:8080
PUBLIC_URL=https://task.example.org
DATABASE_URL=postgres://pushgo_worker:<runtime-password>@postgres:5432/paca?sslmode=disable
PACA_API_KEY_FILE=/run/secrets/paca_api_key
WORKER_SECRET_FILE=/run/secrets/worker_secret
GATEWAY_TOKEN_FILE=/run/secrets/gateway_token
ENCRYPTION_KEY_FILE=/run/secrets/encryption_key
```

`ENCRYPTION_KEY_FILE` must contain the same 64-hex-character AES key as Paca's API uses.
Block `/worker/control` on the public reverse proxy; it is accessed on the private Docker network.
Do not give the worker administrator credentials or access to core database tables.

## MCP tools

- `pushgo_get_reminders`: rule, revision and plan status.
- `pushgo_set_reminders`: precise start/due times, enable flags and lead minutes. Requires the previous rule revision.
- `pushgo_get_delivery_status`: recent jobs and acceptance state.

Each tool first reads the task with the **MCP caller's own personal API key**, then calls permission-protected
plugin routes with the same key. It never substitutes the worker's identity. Unknown or inaccessible tasks fail.

## Limits and recovery

No recurring schedule expansion, check-in, statistics or photos in this release.
Generic integration metadata uses `_integration_state_v1` with `start_precision`, `due_precision`,
`start_instant` / `due_instant`, matching `start_core_date` / `due_core_date` and `timezone`, `archived` and `recurring` values; this is a public task field,
not a dependency on the TaskNotes plugin's private schema.

Network/429/5xx failures retry with capped backoff and expiry. Definitive configuration failures stop automatic
submission. Manual retry preserves `op_id`, requires the current plan and cannot revive an expired reminder.
Gateway credentials and idempotency records must be retained for the full retry window.

There is a final cross-system race between checking Paca and submitting to Gateway. A reminder already accepted
or in flight cannot be promised withdrawn. Its link opens the current official task details.

## Verification

All builds and checks run in GitHub Actions: race tests/vet, formatting, TinyGo, frontend type checks,
MCP bundle, official Paca host/browser loading, and independent TLS Gateway queue acceptance.
The CI Gateway deliberately loses an accepted response to validate stable retries. Production workers cannot
use private Gateway targets; loopback is enabled only by two explicit CI runtime flags in the test job.
Combined acceptance installs a separately verified TaskNotes release, without compiling or merging its code here.
See the Action's `host-verification` report for actual results and remaining device checks.


## 可选拍照打卡与任务卡片

在受控容器网络配置 `CHECKIN_WORKER_URL` 和 `CHECKIN_SERVICE_SECRET_FILE`，与独立打卡插件的内部授权一致。在项目设置开启“在提醒中提供拍照打卡”。未启用时不连接打卡插件。启用后沿用打卡插件确认的精确时刻、窗口和实例版本；结束成功不取消仍有效的独立开始窗口。

网页动作及任务卡片在首次提交前持久保存，超时重试复用同一入口、内容和操作 ID。已冻结的快照不因正文更新而改变，改期或取消会替换计划并让旧入口失效。普通任务可独立使用任务卡片，标题、正文、状态、优先级、准确时间、标签和来源通过通用 `task_card_version`/`task_card` 元数据传递。长消息沿用 Gateway 原有超限拉取机制，不将完整任务正文硬塞到 HMS 直接数据包。

设置及投递页面复用固定版本 shadcn/ui 原组件，所有状态与字段使用中文说明；开发编号、内部状态枚举、SQL 错误和操作 ID 保留在鉴权接口和诊断记录，不显示到日常页面。
