package notification

import (
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "strings"
 "time"
)

var ErrConflict = errors.New("conflict")
var ErrNotFound = errors.New("not found")

type MessageInput struct {
 ExternalID string `json:"external_id"`
 DedupKey string `json:"dedup_key"`
 Recipient string `json:"recipient"`
 Text string `json:"text"`
}

type BatchInput struct {
 Source string `json:"source"`
 SendAt *time.Time `json:"send_at,omitempty"`
 ExpiresAt *time.Time `json:"expires_at,omitempty"`
 Messages []MessageInput `json:"messages"`
}

func (in *BatchInput) Validate() error {
 if len(in.Source) == 0 || len(in.Source) > 100 || len(in.Messages) == 0 || len(in.Messages) > 500 { return errors.New("source and 1 to 500 messages are required") }
 if in.SendAt != nil && in.ExpiresAt != nil && !in.ExpiresAt.After(*in.SendAt) { return errors.New("expires_at must follow send_at") }
 if in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now()) { return errors.New("expires_at must be in the future") }
 seen := map[string]bool{}
 for i := range in.Messages {
  m := &in.Messages[i]
  m.Recipient = strings.TrimPrefix(strings.TrimSpace(m.Recipient), "+")
  if len(m.Recipient) < 8 || len(m.Recipient) > 15 { return errors.New("recipient must include country code and contain 8 to 15 digits") }
  for _, c := range m.Recipient { if c < '0' || c > '9' { return errors.New("recipient must contain only digits") } }
  if m.Recipient[0] == '0' || len(m.DedupKey) == 0 || len(m.DedupKey) > 250 || len(m.ExternalID) > 250 || len(strings.TrimSpace(m.Text)) == 0 || len(m.Text) > 10000 { return errors.New("invalid message, external_id or dedup_key") }
  if seen[m.DedupKey] { return errors.New("dedup_key repeated within batch") }; seen[m.DedupKey] = true
 }
 return nil
}

func inputHash(in BatchInput) string { b, _ := json.Marshal(in); sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func newID() string { var b [16]byte; if _, err := rand.Read(b[:]); err != nil { panic(err) }; return hex.EncodeToString(b[:]) }

type Counts struct {
 Queued int `json:"queued"`
 Sending int `json:"sending"`
 Accepted int `json:"accepted"`
 Rejected int `json:"rejected"`
 Unknown int `json:"unknown"`
 Canceled int `json:"canceled"`
 Expired int `json:"expired"`
}

type Batch struct {
 ID string `json:"id"`
 Source string `json:"source"`
 State string `json:"state"`
 CreatedAt time.Time `json:"created_at"`
 Suppressed int `json:"suppressed"`
 Counts Counts `json:"counts"`
}

type Message struct {
 ID string `json:"id"`
 BatchID string `json:"batch_id"`
 ExternalID string `json:"external_id"`
 Recipient string `json:"recipient"`
 Text string `json:"text"`
 Status string `json:"status"`
 ScheduledAt time.Time `json:"scheduled_at"`
 ErrorCode string `json:"error_code"`
 ErrorMessage string `json:"error_message"`
 ProviderID string `json:"provider_id"`
 AttemptCount int `json:"attempt_count"`
 Retryable bool `json:"retryable"`
 CreatedAt time.Time `json:"created_at"`
 UpdatedAt time.Time `json:"updated_at"`
}

type Attempt struct {
 ID string `json:"id"`
 MessageID string `json:"message_id"`
 Status string `json:"status"`
 Code string `json:"code"`
 Reason string `json:"reason"`
 ProviderID string `json:"provider_id"`
 StartedAt time.Time `json:"started_at"`
 FinishedAt *time.Time `json:"finished_at"`
}

type Delivery struct {
 Status string
 Code string
 Reason string
 ProviderID string
 Retryable bool
 RetryAfter time.Duration
}

type Claim struct { Message Message; AttemptID string }
