package sdk

import (
 "context"
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestEnqueueReusesCallerKeyAndDoesNotClaimDelivery(t *testing.T){
 calls:=0;server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){calls++;if r.Header.Get("Idempotency-Key")!="stable-key"||r.Header.Get("Authorization")!="Bearer fake"{t.Error("missing headers")};w.WriteHeader(202);_,_=w.Write([]byte(`{"id":"batch-1","state":"active","counts":{"queued":1,"accepted":0}}`))}));defer server.Close()
 c:=Client{URL:server.URL,APIKey:"fake"};for i:=0;i<2;i++{b,err:=c.Enqueue(context.Background(),"stable-key",BatchRequest{Source:"test"});if err!=nil||b.Counts.Accepted!=0||b.Counts.Queued!=1{t.Fatal("queued confused with accepted",err)}};if calls!=2{t.Fatal(calls)}
}
func TestClientDoesNotSendCredentialsAcrossRedirect(t *testing.T){
 leaked:=false;target:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){leaked=true}));defer target.Close()
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){http.Redirect(w,r,target.URL,307)}));defer server.Close();c:=Client{URL:server.URL,APIKey:"fake"};if _,err:=c.Enqueue(context.Background(),"key",BatchRequest{});err==nil||leaked{t.Fatal("redirect followed")}
}
