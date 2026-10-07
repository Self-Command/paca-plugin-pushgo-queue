package worker

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model"
	"github.com/jackc/pgx/v5"
)

type settings struct {
	Project  string
	Config   model.Config
	Revision int
	Password string
}

func (w *Worker) Tick(ctx context.Context) error {
	if err := w.control(ctx); err != nil {
		return err
	}
	var project string
	err := w.DB.QueryRow(ctx, "SELECT project_id::text FROM project_settings WHERE needs_reconcile OR last_reconciled IS NULL OR last_reconciled<NOW()-INTERVAL '60 seconds' ORDER BY last_reconciled NULLS FIRST LIMIT 1").Scan(&project)
	if err == nil {
		if err = w.reconcile(ctx, project); err != nil {
			_, _ = w.DB.Exec(ctx, "UPDATE project_settings SET last_error=$1,last_reconciled=NOW(),needs_reconcile=FALSE WHERE project_id=$2", safeError(err), project)
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return w.dispatch(ctx)
}
func safeError(err error) string {
	s := err.Error()
	if len(s) > 400 {
		s = s[:400]
	}
	return s
}
func (w *Worker) loadSettings(ctx context.Context, project string) (settings, error) {
	s := settings{Project: project}
	var config []byte
	err := w.DB.QueryRow(ctx, "SELECT config,revision,password_enc FROM project_settings WHERE project_id=$1", project).Scan(&config, &s.Revision, &s.Password)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(config, &s.Config); err != nil {
		return s, err
	}
	return s, nil
}
func (w *Worker) loadRule(ctx context.Context, project, task string) (*model.Rule, error) {
	var raw []byte
	err := w.DB.QueryRow(ctx, "SELECT config FROM task_rules WHERE project_id=$1 AND task_id=$2", project, task).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r model.Rule
	err = json.Unmarshal(raw, &r)
	return &r, err
}
func (w *Worker) doneStatuses(ctx context.Context, project string) (map[string]bool, error) {
	var list struct {
		Items []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
		} `json:"items"`
	}
	if err := w.call(ctx, "GET", "/projects/"+project+"/task-statuses", nil, &list); err != nil {
		return nil, err
	}
	result := map[string]bool{}
	for _, s := range list.Items {
		result[s.ID] = s.Category == "done"
	}
	return result, nil
}
func (w *Worker) reconcile(ctx context.Context, project string) error {
	var locked bool
	if err := w.DB.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", "reconcile:"+project).Scan(&locked); err != nil || !locked {
		return err
	}
	defer func() {
		_, _ = w.DB.Exec(context.Background(), "SELECT pg_advisory_unlock(hashtextextended($1,0))", "reconcile:"+project)
	}()
	s, err := w.loadSettings(ctx, project)
	if err != nil {
		return err
	}
	if !s.Config.Enabled {
		_, err = w.DB.Exec(ctx, "UPDATE jobs SET state='cancelled',updated_at=NOW() WHERE project_id=$1 AND state IN ('scheduled','retry_wait','sending')", project)
		if err != nil {
			return err
		}
		if _, err = w.DB.Exec(ctx, "UPDATE plans SET state='cancelled',fingerprint='',updated_at=NOW() WHERE project_id=$1", project); err != nil {
			return err
		}
		_, err = w.DB.Exec(ctx, "UPDATE project_settings SET last_reconciled=NOW(),needs_reconcile=FALSE,last_error='' WHERE project_id=$1 AND revision=$2", project, s.Revision)
		return err
	}
	done, err := w.doneStatuses(ctx, project)
	if err != nil {
		return err
	}
	tasks := []model.Task{}
	cursor := ""
	for pages := 0; pages < 10000; pages++ {
		var list struct {
			Items []model.Task `json:"items"`
			Next  *string      `json:"next_cursor"`
		}
		path := "/projects/" + project + "/tasks?page_size=200"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		if err = w.call(ctx, "GET", path, nil, &list); err != nil {
			return err
		}
		tasks = append(tasks, list.Items...)
		if list.Next == nil || *list.Next == "" {
			break
		}
		if *list.Next == cursor || pages == 9999 {
			return errors.New("task reconciliation pagination incomplete")
		}
		cursor = *list.Next
	}
	// Only a complete listing can cancel missing tasks.
	seen := []string{}
	taskError := ""
	for _, task := range tasks {
		// A blocked task must not starve unrelated reminders. Dispatch still checks
		// that individual task and C again before any external submission.
		if err = w.syncTask(ctx, s, task, done[task.StatusID]); err != nil {
			if taskError == "" {
				taskError = safeError(err)
			}
		}
		seen = append(seen, task.ID)
	}
	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "UPDATE jobs SET state='cancelled',updated_at=NOW() WHERE project_id=$1 AND NOT(task_id::text=ANY($2::text[])) AND state IN ('scheduled','retry_wait','sending')", project, seen); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE plans SET state='cancelled',updated_at=NOW() WHERE project_id=$1 AND NOT(task_id::text=ANY($2::text[]))", project, seen); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE project_settings SET last_reconciled=NOW(),needs_reconcile=FALSE,last_error=$3 WHERE project_id=$1 AND revision=$2", project, s.Revision, taskError); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (w *Worker) syncTask(ctx context.Context, s settings, t model.Task, done bool) error {
	rule, err := w.loadRule(ctx, s.Project, t.ID)
	if err != nil {
		return err
	}
	specs, state, err := w.specifications(ctx, s, t, rule, done)
	if err != nil {
		return err
	}
	t.ProjectID = s.Project
	metadata, err := w.taskCardMetadata(ctx, t, s.Config, rule, done)
	if err != nil {
		return err
	}
	fingerprint := model.Hash(map[string]any{"specs": specs, "state": state, "card": metadata})
	tx, err := w.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var revision int
	var oldHash string
	err = tx.QueryRow(ctx, "SELECT revision,fingerprint FROM plans WHERE project_id=$1 AND task_id=$2 FOR UPDATE", s.Project, t.ID).Scan(&revision, &oldHash)
	if err == nil && oldHash == fingerprint {
		if _, err = tx.Exec(ctx, "UPDATE plans SET updated_at=NOW() WHERE project_id=$1 AND task_id=$2", s.Project, t.ID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	revision++
	_, err = tx.Exec(ctx, "INSERT INTO plans(project_id,task_id,fingerprint,revision,state) VALUES($1,$2,$3,$4,$5) ON CONFLICT(project_id,task_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,revision=EXCLUDED.revision,state=EXCLUDED.state,updated_at=NOW()", s.Project, t.ID, fingerprint, revision, state)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE jobs SET state='superseded',updated_at=NOW() WHERE project_id=$1 AND task_id=$2 AND state IN ('scheduled','retry_wait','sending')", s.Project, t.ID); err != nil {
		return err
	}
	for _, spec := range specs {
		payload := map[string]any{"title": spec.Title, "body": spec.Body, "severity": spec.Severity, "ttl": spec.Expires.UnixMilli(), "url": strings.TrimRight(w.PublicURL, "/") + "/projects/" + s.Project + "/tasks/" + t.ID, "metadata": metadata}
		raw, _ := json.Marshal(payload)
		jobState := "scheduled"
		if !spec.Expires.After(time.Now()) {
			jobState = "expired"
		}
		op := model.OpID(s.Project, t.ID, spec)
		// Preserve payload/op_id once a submit was attempted; response-loss retries must remain identical.
		_, err = tx.Exec(ctx, "INSERT INTO jobs(project_id,task_id,kind,target_at,fire_at,expires_at,plan_revision,binding_key,op_id,payload,state,next_attempt) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$5) ON CONFLICT(op_id) DO UPDATE SET plan_revision=EXCLUDED.plan_revision,fire_at=EXCLUDED.fire_at,payload=CASE WHEN jobs.attempts=0 AND NOT jobs.action_ready THEN EXCLUDED.payload ELSE jobs.payload END,state=CASE WHEN jobs.state='gateway_accepted' THEN jobs.state WHEN jobs.expires_at<=NOW() THEN 'expired' ELSE EXCLUDED.state END,next_attempt=GREATEST(EXCLUDED.fire_at,NOW()),lease_owner=NULL,lease_until=NULL,updated_at=NOW()", s.Project, t.ID, spec.Kind, spec.Target, spec.Fire, spec.Expires, revision, spec.Binding, op, string(raw), jobState)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type job struct {
	ID                                      int64
	Project, Task, Kind, Op, Owner, Binding string
	Revision, Attempts, Generation          int
	Payload                                 []byte
	Expires                                 time.Time
	Target                                  time.Time
	ActionReady                             bool
}

func (w *Worker) dispatch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if _, err := w.DB.Exec(ctx, "UPDATE jobs SET state='expired',updated_at=NOW() WHERE expires_at<=NOW() AND state IN ('scheduled','retry_wait','sending')"); err != nil {
		return err
	}
	ownerBytes := make([]byte, 24)
	if _, err := rand.Read(ownerBytes); err != nil {
		return err
	}
	owner := fmt.Sprintf("%x", ownerBytes)
	j := job{Owner: owner}
	err := w.DB.QueryRow(ctx, "WITH candidate AS (SELECT id FROM jobs WHERE state IN ('scheduled','retry_wait','sending') AND fire_at<=NOW() AND next_attempt<=NOW() AND expires_at>NOW() AND (lease_until IS NULL OR lease_until<NOW()) ORDER BY fire_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE jobs j SET state='sending',lease_owner=$1,lease_until=NOW()+INTERVAL '60 seconds',generation=generation+1,updated_at=NOW() FROM candidate c WHERE j.id=c.id RETURNING j.id,j.project_id::text,j.task_id::text,j.kind,j.op_id,j.plan_revision,j.attempts,j.payload,j.expires_at,j.generation,j.binding_key,j.target_at,j.action_ready", owner).Scan(&j.ID, &j.Project, &j.Task, &j.Kind, &j.Op, &j.Revision, &j.Attempts, &j.Payload, &j.Expires, &j.Generation, &j.Binding, &j.Target, &j.ActionReady)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	s, err := w.loadSettings(ctx, j.Project)
	if err != nil {
		return w.result(ctx, j, "retry_wait", "project configuration unavailable", nil, time.Minute)
	}
	var task model.Task
	err = w.call(ctx, "GET", "/projects/"+j.Project+"/tasks/"+j.Task, nil, &task)
	if err != nil {
		var ae apiError
		if errors.As(err, &ae) && ae.Code == 404 {
			return w.result(ctx, j, "cancelled", "task deleted", nil, 0)
		}
		return w.result(ctx, j, "retry_wait", safeError(err), nil, time.Minute)
	}
	done, err := w.doneStatuses(ctx, j.Project)
	if err != nil {
		return w.result(ctx, j, "retry_wait", safeError(err), nil, time.Minute)
	}
	if err = w.syncTask(ctx, s, task, done[task.StatusID]); err != nil {
		return w.result(ctx, j, "retry_wait", safeError(err), nil, 15*time.Second)
	}
	var valid bool
	err = w.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM jobs j JOIN plans p ON p.project_id=j.project_id AND p.task_id=j.task_id WHERE j.id=$1 AND j.state='sending' AND j.lease_owner=$2 AND j.generation=$3 AND j.plan_revision=p.revision AND p.state IN ('active','confirmation_stale') AND j.expires_at>NOW())", j.ID, j.Owner, j.Generation).Scan(&valid)
	if err != nil || !valid {
		return err
	}
	if err = w.control(ctx); err != nil {
		return w.result(ctx, j, "retry_wait", "host control unavailable; paused", nil, time.Minute)
	}
	if strings.Contains(j.Binding, ":checkin:") && j.Attempts == 0 && !j.ActionReady {
		payload, actionErr := w.resolveAction(ctx, j)
		if actionErr != nil {
			return w.result(ctx, j, "retry_wait", safeError(actionErr), nil, 15*time.Second)
		}
		saved, saveErr := w.DB.Exec(ctx, "UPDATE jobs SET payload=$1::jsonb,action_ready=TRUE WHERE id=$2 AND lease_owner=$3 AND generation=$4 AND state='sending' AND attempts=0 AND NOT action_ready AND expires_at>NOW()", string(payload), j.ID, j.Owner, j.Generation)
		if saveErr != nil {
			return saveErr
		}
		if saved.RowsAffected() != 1 {
			return nil
		}
		j.Payload = payload
		j.ActionReady = true
	}
	// Record an attempt before external submit; a crash preserves both op_id and exact payload.
	updated, err := w.DB.Exec(ctx, "UPDATE jobs SET attempts=attempts+1 WHERE id=$1 AND lease_owner=$2 AND generation=$3 AND state='sending' AND EXISTS(SELECT 1 FROM plans p WHERE p.project_id=jobs.project_id AND p.task_id=jobs.task_id AND p.revision=jobs.plan_revision)", j.ID, j.Owner, j.Generation)
	if err != nil {
		return err
	}
	if updated.RowsAffected() != 1 {
		return nil
	}
	accepted, retry, delay, err := w.send(ctx, s, j)
	if err == nil && accepted {
		return w.result(ctx, j, "gateway_accepted", "", retry, 0)
	}
	if err == nil {
		err = errors.New("invalid Gateway acknowledgement")
	}
	state := "retry_wait"
	if delay < 0 || j.Attempts >= 7 {
		state = "failed"
	}
	if delay == 0 {
		delay = time.Duration(1<<min(j.Attempts+1, 8)) * time.Second
	}
	return w.result(ctx, j, state, safeError(err), nil, delay)
}
func (w *Worker) result(ctx context.Context, j job, state, message string, result []byte, delay time.Duration) error {
	var ack any
	if len(result) > 0 {
		ack = string(result)
	}
	_, err := w.DB.Exec(ctx, "UPDATE jobs SET state=$1,last_error=$2,gateway_result=$3::jsonb,next_attempt=NOW()+($4*INTERVAL '1 second'),lease_owner=NULL,lease_until=NULL,updated_at=NOW() WHERE id=$5 AND lease_owner=$6 AND generation=$7 AND (state='sending' OR ($1='gateway_accepted' AND state IN ('superseded','cancelled')))", state, message, ack, delay.Seconds(), j.ID, j.Owner, j.Generation)
	return err
}
