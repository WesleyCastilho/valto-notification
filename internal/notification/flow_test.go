package notification

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPToWorkerAndPersistedReceipt(t *testing.T) {
	s := testStore(t)
	var sends atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"instance":{"state":"open"}}`))
			return
		}
		sends.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"key":{"id":"persisted-receipt"}}`))
	}))
	defer provider.Close()
	tenants := []Tenant{{ID: "bunker", APIKey: strings.Repeat("a", 32), Evolution: Evolution{URL: provider.URL, APIKey: "fake", Instance: "Valto"}}}
	if err := validateTenants(tenants); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := API{Store: s, Tenants: tenants, Log: logger}
	req := httptest.NewRequest(http.MethodPost, "/v1/batches", strings.NewReader(`{"source":"bunker.cadastro-pendencias","messages":[{"external_id":"profile-1","dedup_key":"day:profile-1","recipient":"5535999991234","text":"Reviewed message"}]}`))
	req.Header.Set("Authorization", "Bearer "+tenants[0].APIKey)
	req.Header.Set("Idempotency-Key", "stable-request")
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("enqueue status %d", w.Code)
	}
	var batch Batch
	if err := json.NewDecoder(w.Body).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	if batch.Counts.Queued != 1 || batch.Counts.Accepted != 0 || sends.Load() != 0 {
		t.Fatal("enqueue was confused with provider acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	worker := Worker{Store: s, Tenants: tenants, Interval: time.Second, Enabled: true, Log: logger}
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	for {
		current, err := s.GetBatch(ctx, tenants[0].ID, batch.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Counts.Accepted == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("worker did not save acceptance")
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	<-done
	restarted := &Store{DB: s.DB}
	items, err := restarted.ListMessages(context.Background(), tenants[0].ID, batch.ID, 1)
	if err != nil || len(items) != 1 || items[0].ProviderID != "persisted-receipt" || sends.Load() != 1 {
		t.Fatalf("receipt missing or duplicated: sends=%d err=%v", sends.Load(), err)
	}
	attempts, err := restarted.Attempts(context.Background(), tenants[0].ID, items[0].ID)
	if err != nil || len(attempts) != 1 || attempts[0].Status != "accepted" {
		t.Fatal("attempt not persisted")
	}
}
