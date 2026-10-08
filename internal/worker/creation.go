package worker

import (
	"context"
	"errors"
	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/model"
	"github.com/jackc/pgx/v5"
)

// Remember the first observation even when the option is disabled. Enabling an
// option later, changing a channel or restarting must never replay creations.
func creationSpecs(ctx context.Context,tx pgx.Tx,project string,t model.Task,specs []model.Spec)([]model.Spec,error){
	key:=model.CreationKey(t)
	var created *model.Spec
	for i:=range specs {if specs[i].Kind=="created" {created=&specs[i];break}}
	state:="suppressed"
	op:=""
	if created!=nil {
		var fresh bool
		if err:=tx.QueryRow(ctx,"SELECT $1::timestamptz>=installed_at AND $2::timestamptz>clock_timestamp() FROM creation_cutover WHERE id=1",t.CreatedAt,created.Expires).Scan(&fresh);err!=nil{return nil,err}
		if fresh {state="eligible";op="paca_created_"+model.Hash(project+"\n"+key)}
	}
	if _,err:=tx.Exec(ctx,"INSERT INTO creation_notices(project_id,task_id,logical_key,op_id,state) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING",project,t.ID,key,op,state);err!=nil{return nil,err}
	err:=tx.QueryRow(ctx,"SELECT state,COALESCE(op_id,'') FROM creation_notices WHERE project_id=$1 AND task_id=$2",project,t.ID).Scan(&state,&op)
	if errors.Is(err,pgx.ErrNoRows){state="suppressed"}else if err!=nil{return nil,err}
	if created!=nil&&state=="eligible" {
		var oldBinding string
		err=tx.QueryRow(ctx,"SELECT binding_key FROM jobs WHERE op_id=$1",op).Scan(&oldBinding)
		if err!=nil&&!errors.Is(err,pgx.ErrNoRows){return nil,err}
		if err==nil&&oldBinding!=created.Binding {state="suppressed"}
	}
	out:=[]model.Spec{}
	for _,spec:=range specs {if spec.Kind!="created"||state=="eligible" {out=append(out,spec)}}
	return out,nil
}
