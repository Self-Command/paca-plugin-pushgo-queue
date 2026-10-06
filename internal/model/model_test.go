package model

import("testing";"time")
func TestPrecisionMustMatchCurrentCoreTime(t *testing.T){
 at:=time.Date(2026,10,7,1,0,0,0,time.UTC);task:=Task{Start:&at,Title:"Task"};c:=DefaultConfig()
 specs,state:=Compute(task,c,nil,false);if len(specs)!=0||state!="awaiting_precise_time"{t.Fatal("native date needs confirmation")}
 task.Custom=map[string]any{"_integration_state_v1":map[string]any{"start_precision":"instant","start_instant":at.Format(time.RFC3339)}}
 specs,_=Compute(task,c,nil,false);if len(specs)!=1||!specs[0].Fire.Equal(at.Add(-10*time.Minute)){t.Fatal("proven instant not scheduled")}
 newer:=at.Add(time.Hour);task.Start=&newer;specs,_=Compute(task,c,nil,false);if len(specs)!=0{t.Fatal("stale precision must not schedule new date")}
}
func TestOverrideIdentityAndCancellation(t *testing.T){
 at:=time.Date(2026,10,7,1,0,0,0,time.UTC);task:=Task{Title:"Task"};c:=DefaultConfig();r:=&Rule{Enabled:true,StartEnabled:true,DueEnabled:true,Start:&at,BaseFingerprint:Fingerprint(task)}
 specs,_:=Compute(task,c,r,false);if len(specs)!=1{t.Fatal("explicit confirmation missing")}
 op:=OpID("project","task",specs[0]);task.Title="new title";specs,_=Compute(task,c,r,false);if op!=OpID("project","task",specs[0]){t.Fatal("title edits must retain logical operation identity")}
 specs,_=Compute(task,c,r,true);if len(specs)!=0{t.Fatal("completed tasks must cancel")}
 if len(op)>128{t.Fatal("operation exceeds gateway contract")}
}
func TestFourLevelsAndZeroLeadWindow(t *testing.T){
 c:=DefaultConfig();for v,want:=range map[int]string{0:"normal",10:"low",35:"normal",75:"high",150:"critical"}{if Severity(v,c.PriorityMap)!=want{t.Fatalf("priority %d",v)}}
 at:=time.Now();zero:=0;task:=Task{};r:=&Rule{Enabled:true,StartEnabled:true,Start:&at,StartMinutes:&zero,BaseFingerprint:Fingerprint(task)};specs,_:=Compute(task,c,r,false);if !specs[0].Expires.After(specs[0].Fire){t.Fatal("zero lead requires non-empty delivery window")}
}
