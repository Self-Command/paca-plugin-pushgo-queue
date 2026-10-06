package main

import (
	plugin "github.com/Paca-AI/plugin-sdk-go"
	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/buildinfo"
)

const pluginID = "com.selfcommand.pushgo-queue"
const pluginVersion = buildinfo.Version

type integrationPlugin struct {
	db  *plugin.DB
	cfg *plugin.Config
}

func (p *integrationPlugin) Init(ctx *plugin.Context) error {
	p.db = ctx.DB()
	p.cfg = ctx.Config()
	ctx.Route("POST", "/admin/worker-credential", p.rotateWorkerCredential)
	ctx.Route("GET", "/worker/control", p.workerControl)
	ctx.Route("GET", "/health", p.health)
	ctx.Route("GET", "/projects/:projectId/status", p.status)
	ctx.Route("GET", "/projects/:projectId/settings", p.settings)
	ctx.Route("PUT", "/projects/:projectId/settings", p.saveSettings)
	ctx.Route("GET", "/projects/:projectId/jobs", p.jobs)
	ctx.Route("POST", "/projects/:projectId/jobs/:jobId/retry", p.retry)
	ctx.Route("GET", "/projects/:projectId/tasks/:taskId/reminders", p.reminders)
	ctx.Route("PUT", "/projects/:projectId/tasks/:taskId/reminders", p.setReminders)
	ctx.Route("GET", "/projects/:projectId/tasks/:taskId/deliveries", p.jobs)
	for _,topic:=range []string{"task.created","task.updated","task.deleted"}{ctx.On(topic,p.dirty)}
	return nil
}
func (p *integrationPlugin) Shutdown() {}
func (p *integrationPlugin) health(req *plugin.Request, res *plugin.Response) {
	result, err := p.db.Query("SELECT version FROM plugin_metadata WHERE id = 1")
	if err != nil || len(result.Rows) != 1 {
		res.Error(503, "plugin migration unavailable")
		return
	}
	res.JSON(200, map[string]any{"id": pluginID, "version": pluginVersion, "source_sha": buildinfo.SourceSHA, "schema_version": result.Rows[0][0], "phase": "pushgo-queue"})
}
func (p *integrationPlugin) status(req *plugin.Request, res *plugin.Response) {
	p.settings(req,res)
}
