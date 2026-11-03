package notification

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestEvolutionDeliveryClassification(t *testing.T){
 cases:=[]struct{name string;code int;body,status string;retry bool}{
  {"accepted",201,`{"key":{"id":"provider-123"}}`,"accepted",false},
  {"instance missing",404,`{"message":"The instance does not exist","apikey":"secret-never-log"}`,"rejected",true},
  {"server error",503,`{"error":"failed"}`,"unknown",false},
  {"timeout response",408,`{}`,"unknown",false},
  {"invalid success",200,`{"status":"ok"}`,"unknown",false},
  {"rate limited",429,`{}`,"rejected",true},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   if r.Method!="POST"||r.URL.Path!="/message/sendText/Valto"||r.Header.Get("apikey")!="fake-key"{t.Errorf("wrong transport")}
   var payload map[string]string;if json.NewDecoder(r.Body).Decode(&payload)!=nil||payload["number"]!="5535999991234"||payload["text"]!="Hello"{t.Errorf("wrong payload")}
   w.Header().Set("Retry-After","60");w.WriteHeader(tc.code);_,_=w.Write([]byte(tc.body))
  }));defer server.Close()
  e:=Evolution{URL:server.URL,APIKey:"fake-key",Instance:"Valto"};d:=e.Send(context.Background(),"5535999991234","Hello")
  if d.Status!=tc.status||d.Retryable!=tc.retry||strings.Contains(d.Reason,"secret-never-log"){t.Fatalf("unexpected classification: %+v",d)}
  if tc.status=="accepted"&&d.ProviderID!="provider-123"{t.Fatal("receipt missing")};if tc.code==429&&d.RetryAfter.Seconds()!=60{t.Fatal("Retry-After ignored")}
 })}
}
func TestEvolutionDoesNotFollowRedirect(t *testing.T){
 received:=false;target:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){received=true}));defer target.Close()
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){http.Redirect(w,r,target.URL,http.StatusTemporaryRedirect)}));defer server.Close()
 e:=Evolution{URL:server.URL,APIKey:"secret",Instance:"Valto"};if e.Send(context.Background(),"5535999991234","Hello").Status!="unknown"||received{t.Fatal("followed redirect")}
}
func TestEvolutionProbe(t *testing.T){
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){if r.URL.Path!="/instance/connectionState/Valto"{t.Error("wrong probe")};_,_=w.Write([]byte(`{"instance":{"state":"open"}}`))}));defer server.Close()
 if result:=(Evolution{URL:server.URL,Instance:"Valto",APIKey:"fake"}).Probe(context.Background());result.Status!="ready"{t.Fatal(result)}
}
func TestConfigurationRejectsDuplicateClientsAndUnsafeURLs(t *testing.T){
 tenant:=Tenant{ID:"bunker",APIKey:strings.Repeat("a",32),Evolution:Evolution{URL:"https://example.invalid",APIKey:"fake",Instance:"Valto"}}
 if err:=validateTenants([]Tenant{tenant});err!=nil{t.Fatal(err)}
 if validateTenants([]Tenant{tenant,tenant})==nil{t.Fatal("duplicate client accepted")}
 tenant.Evolution.URL="https://example.invalid?key=secret";if validateTenants([]Tenant{tenant})==nil{t.Fatal("unsafe URL accepted")}
}
