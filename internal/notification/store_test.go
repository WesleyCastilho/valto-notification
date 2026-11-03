package notification

import (
 "context"
 "database/sql"
 "errors"
 "io"
 "log/slog"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "sync"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/stdlib"
)

func testStore(t *testing.T)*Store{
 t.Helper();dsn:=os.Getenv("TEST_DATABASE_URL");if dsn==""{t.Skip("TEST_DATABASE_URL not configured")}
 admin,err:=sql.Open("pgx",dsn);if err!=nil{t.Fatal(err)}
 name:="valto_test_"+newID();if _,err=admin.Exec("CREATE SCHEMA "+name);err!=nil{t.Fatal(err)}
 cfg,err:=pgx.ParseConfig(dsn);if err!=nil{t.Fatal(err)};cfg.RuntimeParams["search_path"]=name;db:=stdlib.OpenDB(*cfg);db.SetMaxOpenConns(8)
 t.Cleanup(func(){_ = db.Close();_,_=admin.Exec("DROP SCHEMA "+name+" CASCADE");_ = admin.Close()})
 store:=&Store{DB:db};if err:=store.Migrate(context.Background());err!=nil{t.Fatal(err)};if err:=store.Migrate(context.Background());err!=nil{t.Fatal("migration not repeatable",err)};return store
}
func input(keys ...string)BatchInput{in:=BatchInput{Source:"test"};for _,key:=range keys{in.Messages=append(in.Messages,MessageInput{ExternalID:key,DedupKey:key,Recipient:"5535999991234",Text:"Hello"})};return in}
func create(t *testing.T,s *Store,tenant,key string,in BatchInput)Batch{t.Helper();if err:=in.Validate();err!=nil{t.Fatal(err)};b,_,err:=s.CreateBatch(context.Background(),tenant,key,in);if err!=nil{t.Fatal(err)};return b}
func claim(t *testing.T,s *Store,tenant,lane string)*Claim{t.Helper();c,err:=s.Claim(context.Background(),tenant,lane,time.Millisecond);if err!=nil||c==nil{t.Fatalf("claim: %v",err)};return c}
func finish(t *testing.T,s *Store,tenant string,c *Claim,d Delivery){t.Helper();if err:=s.Finish(context.Background(),tenant,*c,d,3);err!=nil{t.Fatal(err)}}

func TestBatchIdempotencyDedupAndIsolation(t *testing.T){
 s:=testStore(t);ctx:=context.Background();in:=input("day:profile1")
 b:=create(t,s,"a","request1",in)
 same,replayed,err:=s.CreateBatch(ctx,"a","request1",in);if err!=nil||!replayed||same.ID!=b.ID{t.Fatal("not idempotent",err)}
 changed:=input("day:profile1");changed.Messages[0].Text="changed";if _,_,err:=s.CreateBatch(ctx,"a","request1",changed);!errors.Is(err,ErrConflict){t.Fatal("different request accepted")}
 duplicate:=create(t,s,"a","request2",in);if duplicate.Suppressed!=1||duplicate.Counts.Queued!=0{t.Fatal("dedup failed")}
 other:=create(t,s,"b","request1",in);if other.Counts.Queued!=1{t.Fatal("cross tenant dedup")}
 if _,err:=s.GetBatch(ctx,"b",b.ID);!errors.Is(err,ErrNotFound){t.Fatal("batch leaked")}
 list,_:=s.ListMessages(ctx,"a",b.ID,1);if _,err:=s.GetMessage(ctx,"b",list[0].ID);!errors.Is(err,ErrNotFound){t.Fatal("message leaked")}
}

func TestConcurrentCreateAndClaim(t *testing.T){
 s:=testStore(t);ctx:=context.Background();var wg sync.WaitGroup;results:=make(chan string,4)
 for i:=0;i<4;i++{wg.Add(1);go func(){defer wg.Done();b,_,err:=s.CreateBatch(ctx,"a","same",input("one"));if err!=nil{results<-"error"}else{results<-b.ID}}()};wg.Wait();close(results)
 id:="";for got:=range results{if id==""{id=got};if got!=id||got=="error"{t.Fatal("concurrent duplicate batches")}}
 var claims int;var mu sync.Mutex
 for i:=0;i<4;i++{wg.Add(1);go func(){defer wg.Done();c,err:=s.Claim(ctx,"a","shared-lane",time.Second);if err!=nil{t.Error(err)};if c!=nil{mu.Lock();claims++;mu.Unlock()}}()};wg.Wait();if claims!=1{t.Fatalf("claims=%d",claims)}
}

func TestAttemptsSurviveRetryAndRestart(t *testing.T){
 s:=testStore(t);ctx:=context.Background();create(t,s,"a","r1",input("one"));c:=claim(t,s,"a","lane")
 finish(t,s,"a",c,Delivery{Status:"rejected",Code:"http_404",Reason:"instance missing",Retryable:true})
 reopened:=&Store{DB:s.DB};if err:=reopened.Retry(ctx,"a",c.Message.ID);err!=nil{t.Fatal(err)};time.Sleep(2*time.Millisecond)
 second:=claim(t,reopened,"a","lane");finish(t,reopened,"a",second,Delivery{Status:"accepted",ProviderID:"receipt-1"})
 attempts,err:=reopened.Attempts(ctx,"a",c.Message.ID);if err!=nil||len(attempts)!=2||attempts[0].Status!="rejected"||attempts[1].ProviderID!="receipt-1"{t.Fatalf("attempts lost: %+v %v",attempts,err)}
 if err:=reopened.Retry(ctx,"a",c.Message.ID);!errors.Is(err,ErrConflict){t.Fatal("accepted message retried")}
}

func TestLeaseRecoveryDoesNotResendUnknown(t *testing.T){
 s:=testStore(t);ctx:=context.Background();create(t,s,"a","r",input("one"));c:=claim(t,s,"a","lane")
 if _,err:=s.DB.Exec("UPDATE valto_messages SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1",c.Message.ID);err!=nil{t.Fatal(err)}
 if err:=s.Recover(ctx);err!=nil{t.Fatal(err)};m,_:=s.GetMessage(ctx,"a",c.Message.ID);if m.Status!="unknown"{t.Fatal(m.Status)}
 if err:=s.Retry(ctx,"a",m.ID);!errors.Is(err,ErrConflict){t.Fatal("uncertain send retried")}
 attempts,_:=s.Attempts(ctx,"a",m.ID);if len(attempts)!=1||attempts[0].Status!="unknown"{t.Fatal(attempts)}
}

func TestSchedulingPauseCancelAndRateLimit(t *testing.T){
 s:=testStore(t);ctx:=context.Background();future:=time.Now().Add(time.Hour);in:=input("future");in.SendAt=&future;b:=create(t,s,"a","future",in)
 if due,_:=s.HasDue(ctx,"a");due{t.Fatal("future job is due")}
 if err:=s.ChangeBatch(ctx,"a",b.ID,"canceled");err!=nil{t.Fatal(err)};if err:=s.ChangeBatch(ctx,"a",b.ID,"active");!errors.Is(err,ErrConflict){t.Fatal("canceled batch resumed")}
 now:=create(t,s,"a","now",input("now"));if err:=s.ChangeBatch(ctx,"a",now.ID,"paused");err!=nil{t.Fatal(err)};if due,_:=s.HasDue(ctx,"a");due{t.Fatal("paused job is due")}
 _ = s.ChangeBatch(ctx,"a",now.ID,"active");c:=claim(t,s,"a","lane");finish(t,s,"a",c,Delivery{Status:"rejected",Code:"http_429",Reason:"rate limit",Retryable:true,RetryAfter:time.Minute})
 m,_:=s.GetMessage(ctx,"a",c.Message.ID);if m.Status!="queued"||!m.ScheduledAt.After(time.Now().Add(45*time.Second)){t.Fatal("rate limit not delayed")}
 create(t,s,"a","expire",input("expire"));_,_=s.DB.Exec("UPDATE valto_messages SET expires_at=NOW()-INTERVAL '1 second' WHERE dedup_key='expire'");if err:=s.Recover(ctx);err!=nil{t.Fatal(err)}
 var state string;_ = s.DB.QueryRow("SELECT status FROM valto_messages WHERE dedup_key='expire'").Scan(&state);if state!="expired"{t.Fatal(state)}
}

func TestAPIAuthenticationAndCrossTenantAccess(t *testing.T){
 s:=testStore(t);b:=create(t,s,"a","r",input("one"));tenants:=[]Tenant{{ID:"a",APIKey:strings.Repeat("a",32),Evolution:Evolution{URL:"https://example.invalid",APIKey:"fake",Instance:"test"}},{ID:"b",APIKey:strings.Repeat("b",32),Evolution:Evolution{URL:"https://example.invalid",APIKey:"fake",Instance:"test"}}};if err:=validateTenants(tenants);err!=nil{t.Fatal(err)}
 api:=API{Store:s,Tenants:tenants,Log:slog.New(slog.NewTextHandler(io.Discard,nil))};h:=api.Handler()
 for _,tc:=range []struct{token string;want int}{{"",401},{"bad",401},{strings.Repeat("b",32),404},{strings.Repeat("a",32),200}}{
  req:=httptest.NewRequest(http.MethodGet,"/v1/batches/"+b.ID,nil);if tc.token!=""{req.Header.Set("Authorization","Bearer "+tc.token)};w:=httptest.NewRecorder();h.ServeHTTP(w,req);if w.Code!=tc.want{t.Fatalf("got %d want %d",w.Code,tc.want)}
 }
 req:=httptest.NewRequest(http.MethodPost,"/v1/batches",strings.NewReader(`{"source":"x","messages":[]}`));req.Header.Set("Authorization","Bearer "+strings.Repeat("a",32));w:=httptest.NewRecorder();h.ServeHTTP(w,req);if w.Code!=400{t.Fatal("missing idempotency key accepted")}
}

func TestWorkerDisabledAndUnhealthyProviderDoNotSend(t *testing.T){
 s:=testStore(t);create(t,s,"a","r",input("one"));calls:=0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.Method=="POST"{calls++};w.WriteHeader(404)}));defer server.Close()
 worker:=Worker{Store:s,Tenants:[]Tenant{{ID:"a",Evolution:Evolution{URL:server.URL,Instance:"test",APIKey:"fake"}}},Interval:time.Second,Enabled:false,Log:slog.New(slog.NewTextHandler(io.Discard,nil))}
 ctx,cancel:=context.WithTimeout(context.Background(),40*time.Millisecond);worker.Run(ctx);cancel();if calls!=0{t.Fatal("standby sent")}
 worker.Enabled=true;ctx,cancel=context.WithTimeout(context.Background(),80*time.Millisecond);worker.Run(ctx);cancel();if calls!=0{t.Fatal("unhealthy provider sent")}
 var attempts int;_ = s.DB.QueryRow("SELECT count(*) FROM valto_attempts").Scan(&attempts);if attempts!=0{t.Fatal("probe consumed a message")}
}
