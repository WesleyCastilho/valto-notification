package notification

import (
 "context"
 "crypto/sha256"
 "crypto/subtle"
 "encoding/json"
 "errors"
 "io"
 "log/slog"
 "net/http"
 "strconv"
 "strings"
 "time"
)

type API struct { Store *Store; Tenants []Tenant; DeliveryEnabled bool; WorkerOnly bool; Log *slog.Logger }
type apiError struct { Code string `json:"code"`; Message string `json:"message"`; RequestID string `json:"request_id"` }
func writeJSON(w http.ResponseWriter,status int,value any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);_ = json.NewEncoder(w).Encode(value)}
func fail(w http.ResponseWriter,r *http.Request,status int,code,message string){writeJSON(w,status,map[string]any{"error":apiError{code,message,w.Header().Get("X-Request-ID")}})}
func decode(w http.ResponseWriter,r *http.Request,v any)bool{
 d:=json.NewDecoder(http.MaxBytesReader(w,r.Body,1<<20));d.DisallowUnknownFields()
 if d.Decode(v)!=nil{fail(w,r,400,"invalid_json","A JSON object up to 1 MiB is required.");return false}
 if d.Decode(&struct{}{})!=io.EOF{fail(w,r,400,"invalid_json","Only one JSON object is allowed.");return false};return true
}

func(a *API)auth(next func(http.ResponseWriter,*http.Request,Tenant))http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){
 header:=r.Header.Get("Authorization");if !strings.HasPrefix(header,"Bearer "){fail(w,r,401,"unauthorized","A client bearer key is required.");return};hash:=sha256.Sum256([]byte(strings.TrimPrefix(header,"Bearer ")))
 for _,t:=range a.Tenants{if subtle.ConstantTimeCompare(hash[:],t.TokenHash[:])==1{next(w,r,t);return}}
 fail(w,r,401,"unauthorized","Invalid client key.")
}}

func(a *API)Handler()http.Handler{
 mux:=http.NewServeMux()
 mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,r *http.Request){writeJSON(w,200,map[string]string{"status":"ok"})})
 mux.HandleFunc("GET /readyz",func(w http.ResponseWriter,r *http.Request){ctx,cancel:=context.WithTimeout(r.Context(),2*time.Second);defer cancel();if a.Store.DB.PingContext(ctx)!=nil{fail(w,r,503,"database_unavailable","Database unavailable.");return};writeJSON(w,200,map[string]any{"status":"ready","delivery_enabled":a.DeliveryEnabled})})
 if !a.WorkerOnly{
 mux.HandleFunc("POST /v1/batches",a.auth(a.createBatch))
 mux.HandleFunc("GET /v1/batches/{id}",a.auth(a.getBatch))
 mux.HandleFunc("PATCH /v1/batches/{id}",a.auth(a.changeBatch))
 mux.HandleFunc("GET /v1/batches/{id}/messages",a.auth(a.batchMessages))
 mux.HandleFunc("GET /v1/messages/{id}",a.auth(a.getMessage))
 mux.HandleFunc("GET /v1/messages/{id}/attempts",a.auth(a.attempts))
 mux.HandleFunc("POST /v1/messages/{id}/retry",a.auth(a.retry))
 mux.HandleFunc("GET /v1/provider/health",a.auth(func(w http.ResponseWriter,r *http.Request,t Tenant){result:=t.Evolution.Probe(r.Context());writeJSON(w,200,map[string]any{"status":result.Status,"code":result.Code,"reason":result.Reason,"delivery_enabled":a.DeliveryEnabled})}))
 }
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  id:=newID();w.Header().Set("X-Request-ID",id);w.Header().Set("Cache-Control","no-store")
  defer func(){if recover()!=nil{a.Log.Error("request_panicked","request_id",id);fail(w,r,500,"internal_error","Internal error.")}}()
  mux.ServeHTTP(w,r)
 })
}

func(a *API)storeError(w http.ResponseWriter,r *http.Request,err error){
 if errors.Is(err,ErrNotFound){fail(w,r,404,"not_found","Resource not found.");return}
 if errors.Is(err,ErrConflict){fail(w,r,409,"conflict","The key or current resource state conflicts with this request.");return}
 a.Log.Error("storage_operation_failed","request_id",w.Header().Get("X-Request-ID"));fail(w,r,500,"storage_error","Operation could not be completed; retry with the same idempotency key.")
}
func(a *API)createBatch(w http.ResponseWriter,r *http.Request,t Tenant){
 key:=r.Header.Get("Idempotency-Key");if len(strings.TrimSpace(key))==0||len(key)>200{fail(w,r,400,"invalid_idempotency_key","Idempotency-Key with 1 to 200 characters is required.");return}
 var in BatchInput;if !decode(w,r,&in){return};if err:=in.Validate();err!=nil{fail(w,r,400,"invalid_batch",err.Error());return}
 b,replay,err:=a.Store.CreateBatch(r.Context(),t.ID,key,in);if err!=nil{a.storeError(w,r,err);return}
 w.Header().Set("Location","/v1/batches/"+b.ID);status:=http.StatusAccepted;if replay{status=http.StatusOK}
 a.Log.Info("batch_enqueued","tenant",t.ID,"batch_id",b.ID,"replay",replay,"request_id",w.Header().Get("X-Request-ID"));writeJSON(w,status,b)
}
func(a *API)getBatch(w http.ResponseWriter,r *http.Request,t Tenant){b,err:=a.Store.GetBatch(r.Context(),t.ID,r.PathValue("id"));if err!=nil{a.storeError(w,r,err);return};writeJSON(w,200,b)}
func(a *API)changeBatch(w http.ResponseWriter,r *http.Request,t Tenant){
 if _,err:=a.Store.GetBatch(r.Context(),t.ID,r.PathValue("id"));err!=nil{a.storeError(w,r,err);return}
 var in struct{State string `json:"state"`};if !decode(w,r,&in){return};if in.State!="active"&&in.State!="paused"&&in.State!="canceled"{fail(w,r,400,"invalid_state","Use active, paused or canceled.");return}
 if err:=a.Store.ChangeBatch(r.Context(),t.ID,r.PathValue("id"),in.State);err!=nil{a.storeError(w,r,err);return};a.getBatch(w,r,t)
}
func(a *API)batchMessages(w http.ResponseWriter,r *http.Request,t Tenant){
 if _,err:=a.Store.GetBatch(r.Context(),t.ID,r.PathValue("id"));err!=nil{a.storeError(w,r,err);return}
 page:=1;var err error;if value:=r.URL.Query().Get("page");value!=""{page,err=strconv.Atoi(value)};if err!=nil||page<1||page>1000000{fail(w,r,400,"invalid_page","Invalid page.");return}
 items,err:=a.Store.ListMessages(r.Context(),t.ID,r.PathValue("id"),page);if err!=nil{a.storeError(w,r,err);return};writeJSON(w,200,map[string]any{"items":items,"page":page,"page_size":50})
}
func(a *API)getMessage(w http.ResponseWriter,r *http.Request,t Tenant){m,err:=a.Store.GetMessage(r.Context(),t.ID,r.PathValue("id"));if err!=nil{a.storeError(w,r,err);return};writeJSON(w,200,m)}
func(a *API)attempts(w http.ResponseWriter,r *http.Request,t Tenant){if _,err:=a.Store.GetMessage(r.Context(),t.ID,r.PathValue("id"));err!=nil{a.storeError(w,r,err);return};items,err:=a.Store.Attempts(r.Context(),t.ID,r.PathValue("id"));if err!=nil{a.storeError(w,r,err);return};writeJSON(w,200,map[string]any{"items":items})}
func(a *API)retry(w http.ResponseWriter,r *http.Request,t Tenant){if _,err:=a.Store.GetMessage(r.Context(),t.ID,r.PathValue("id"));err!=nil{a.storeError(w,r,err);return};if err:=a.Store.Retry(r.Context(),t.ID,r.PathValue("id"));err!=nil{a.storeError(w,r,err);return};a.Log.Info("manual_retry_enqueued","tenant",t.ID,"message_id",r.PathValue("id"));a.getMessage(w,r,t)}
