package notification

import (
 "context"
 "log/slog"
 "time"
)

type Worker struct { Store *Store; Tenants []Tenant; Interval time.Duration; Enabled bool; Log *slog.Logger }
type probeCache struct{checked time.Time; result Delivery}

func(w *Worker)Run(ctx context.Context){
 if !w.Enabled{w.Log.Info("delivery_disabled");<-ctx.Done();return}
 cache:=map[string]probeCache{}
 ticker:=time.NewTicker(500*time.Millisecond);defer ticker.Stop()
 for{
  if err:=w.Store.Recover(ctx);err!=nil&&ctx.Err()==nil{w.Log.Error("queue_recovery_failed")}
  for _,t:=range w.Tenants{
   if ctx.Err()!=nil{return}
   due,err:=w.Store.HasDue(ctx,t.ID);if err!=nil{w.Log.Error("queue_check_failed","tenant",t.ID);continue};if !due{continue}
   probe:=cache[t.ID]
   if time.Since(probe.checked)>30*time.Second{
    probe=probeCache{checked:time.Now(),result:t.Evolution.Probe(ctx)};cache[t.ID]=probe
    if probe.result.Status!="ready"{w.Log.Warn("provider_blocked","tenant",t.ID,"code",probe.result.Code)}
   }
   if probe.result.Status!="ready"{continue}
   c,err:=w.Store.Claim(ctx,t.ID,t.Evolution.Lane(),w.Interval);if err!=nil{w.Log.Error("claim_failed","tenant",t.ID);continue};if c==nil{continue}
   w.Log.Info("send_started","tenant",t.ID,"batch_id",c.Message.BatchID,"message_id",c.Message.ID,"attempt_id",c.AttemptID)
   result:=t.Evolution.Send(ctx,c.Message.Recipient,c.Message.Text)
   saveCtx,cancel:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
   err=w.Store.Finish(saveCtx,t.ID,*c,result,3);cancel()
   if err!=nil{w.Log.Error("result_persistence_failed","tenant",t.ID,"message_id",c.Message.ID)}else{w.Log.Info("send_finished","tenant",t.ID,"message_id",c.Message.ID,"attempt_id",c.AttemptID,"status",result.Status,"code",result.Code)}
   if result.Code=="http_401"||result.Code=="http_403"||result.Code=="http_404"{delete(cache,t.ID)}
  }
  select{case<-ctx.Done():return;case<-ticker.C:}
 }
}
