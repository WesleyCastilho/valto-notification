// Package sdk is a server-side client. Never expose the service bearer key in a browser.
package sdk

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

type Message struct {ExternalID string `json:"external_id"`;DedupKey string `json:"dedup_key"`;Recipient string `json:"recipient"`;Text string `json:"text"`}
type BatchRequest struct{Source string `json:"source"`;SendAt *time.Time `json:"send_at,omitempty"`;ExpiresAt *time.Time `json:"expires_at,omitempty"`;Messages []Message `json:"messages"`}
type Counts struct{Queued int `json:"queued"`;Sending int `json:"sending"`;Accepted int `json:"accepted"`;Rejected int `json:"rejected"`;Unknown int `json:"unknown"`;Canceled int `json:"canceled"`;Expired int `json:"expired"`}
type Batch struct{ID string `json:"id"`;State string `json:"state"`;Source string `json:"source"`;Suppressed int `json:"suppressed"`;Counts Counts `json:"counts"`}
type Client struct{URL,APIKey string;HTTP *http.Client}
type Error struct{Status int;Code,Message string}
func(e *Error)Error()string{return fmt.Sprintf("Valto returned HTTP %d (%s)",e.Status,e.Code)}

func(c *Client)request(ctx context.Context,method,path,key string,in,out any)error{
 base,err:=url.Parse(c.URL);if err!=nil||base.Host==""||base.User!=nil||base.RawQuery!=""||base.Fragment!=""||(base.Scheme!="https"&&!(base.Scheme=="http"&&(base.Hostname()=="localhost"||base.Hostname()=="127.0.0.1"))){return errors.New("invalid Valto URL")}
 if c.APIKey==""{return errors.New("Valto client key is required")}
 var body []byte;if in!=nil{body,err=json.Marshal(in);if err!=nil{return err}}
 req,err:=http.NewRequestWithContext(ctx,method,strings.TrimRight(c.URL,"/")+path,bytes.NewReader(body));if err!=nil{return errors.New("invalid Valto request")};req.Header.Set("Authorization","Bearer "+c.APIKey);req.Header.Set("Content-Type","application/json");if key!=""{req.Header.Set("Idempotency-Key",key)}
 client:=http.Client{Timeout:20*time.Second};if c.HTTP!=nil{client=*c.HTTP;if client.Timeout==0{client.Timeout=20*time.Second}};client.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
 resp,err:=client.Do(req);if err!=nil{return errors.New("Valto result uncertain; retry using the same idempotency key, never fall back to another sender")};defer resp.Body.Close()
 data,err:=io.ReadAll(io.LimitReader(resp.Body,2<<20));if err!=nil{return errors.New("Valto response failed; retry using the same idempotency key")}
 if resp.StatusCode<200||resp.StatusCode>=300{var envelope struct{Error struct{Code,Message string} `json:"error"`};_ = json.Unmarshal(data,&envelope);return &Error{Status:resp.StatusCode,Code:envelope.Error.Code,Message:envelope.Error.Message}}
 if err=json.Unmarshal(data,out);err!=nil{return errors.New("invalid Valto response; retry using the same idempotency key")};return nil
}

// Enqueue means queued, not accepted by Evolution or delivered to WhatsApp.
func(c *Client)Enqueue(ctx context.Context,key string,in BatchRequest)(Batch,error){if strings.TrimSpace(key)==""{return Batch{},errors.New("idempotency key is required")};var b Batch;err:=c.request(ctx,http.MethodPost,"/v1/batches",key,in,&b);return b,err}
func(c *Client)Batch(ctx context.Context,id string)(Batch,error){var b Batch;err:=c.request(ctx,http.MethodGet,"/v1/batches/"+url.PathEscape(id),"",nil,&b);return b,err}
func(c *Client)ChangeBatch(ctx context.Context,id,state string)(Batch,error){var b Batch;err:=c.request(ctx,http.MethodPatch,"/v1/batches/"+url.PathEscape(id),"",map[string]string{"state":state},&b);return b,err}

// BunkerDailyKey matches the existing per-profile/date boundary for cutover.
// Date must be computed in America/Sao_Paulo by Bunker, not by the transport.
func BunkerDailyKey(day,profileID string)string{return "bunker:cadastro-pendencias:"+day+":"+profileID}
