package notification

// The Evolution sendText transport was extracted from CT BunkerBJJ's
// backend/internal/usecase/whatsapp/notification.go (commit 7899e5b6).
// Registration rules and message composition deliberately stay in Bunker.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Evolution struct {
	URL      string       `json:"url"`
	APIKey   string       `json:"api_key"`
	Instance string       `json:"instance"`
	HTTP     *http.Client `json:"-"`
}

func (e Evolution) Lane() string {
	s := sha256.Sum256([]byte(strings.TrimRight(e.URL, "/") + "/" + e.Instance))
	return hex.EncodeToString(s[:])
}
func (e Evolution) request(ctx context.Context, method, path string, payload any) (int, []byte, http.Header, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(e.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("apikey", e.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 15 * time.Second}
	if e.HTTP != nil {
		client = *e.HTTP
		if client.Timeout == 0 {
			client.Timeout = 15 * time.Second
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil {
		return resp.StatusCode, nil, resp.Header, err
	}
	if len(data) > 65536 {
		return resp.StatusCode, nil, resp.Header, fmt.Errorf("response too large")
	}
	return resp.StatusCode, data, resp.Header, nil
}

func rejection(code int, body []byte) Delivery {
	d := Delivery{Status: "rejected", Code: fmt.Sprintf("http_%d", code), Reason: "Evolution rejected the request.", Retryable: true}
	switch code {
	case 401, 403:
		d.Reason = "Check Evolution API key and instance permission."
	case 404:
		d.Reason = "Check Evolution URL and instance name."
		lower := strings.ToLower(string(body))
		if strings.Contains(lower, "instance") && (strings.Contains(lower, "not exist") || strings.Contains(lower, "not found") || strings.Contains(lower, "doesn't exist")) {
			d.Reason = "Evolution instance does not exist on this server."
		}
	case 429:
		d.Reason = "Evolution rate limit reached."
	case 400:
		d.Reason = "Evolution rejected the recipient or message."
	}
	return d
}

func (e Evolution) Probe(ctx context.Context) Delivery {
	code, body, _, err := e.request(ctx, http.MethodGet, "/instance/connectionState/"+url.PathEscape(e.Instance), nil)
	if err != nil {
		return Delivery{Status: "blocked", Code: "provider_unreachable", Reason: "Could not check Evolution connection."}
	}
	if code < 200 || code >= 300 {
		d := rejection(code, body)
		d.Status = "blocked"
		return d
	}
	var state struct {
		Instance struct {
			State string `json:"state"`
		} `json:"instance"`
	}
	if json.Unmarshal(body, &state) != nil || state.Instance.State != "open" {
		return Delivery{Status: "blocked", Code: "instance_not_open", Reason: "Evolution instance is not connected."}
	}
	return Delivery{Status: "ready"}
}

func (e Evolution) Send(ctx context.Context, recipient, text string) Delivery {
	code, body, headers, err := e.request(ctx, http.MethodPost, "/message/sendText/"+url.PathEscape(e.Instance), map[string]string{"number": recipient, "text": text})
	if err != nil {
		return Delivery{Status: "unknown", Code: "transport_unknown", Reason: "Connection or response failed; reconcile before retrying."}
	}
	if code >= 400 && code < 500 && code != 408 {
		d := rejection(code, body)
		if code == 429 {
			if seconds, err := strconv.Atoi(headers.Get("Retry-After")); err == nil && seconds > 0 && seconds <= 3600 {
				d.RetryAfter = time.Duration(seconds) * time.Second
			}
		}
		return d
	}
	if code < 200 || code >= 300 {
		return Delivery{Status: "unknown", Code: fmt.Sprintf("http_%d", code), Reason: "Provider result is uncertain; reconcile before retrying."}
	}
	var receipt struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
	}
	if json.Unmarshal(body, &receipt) != nil || receipt.Key.ID == "" {
		return Delivery{Status: "unknown", Code: "missing_receipt", Reason: "Evolution response did not contain a message ID; reconcile before retrying."}
	}
	return Delivery{Status: "accepted", ProviderID: receipt.Key.ID}
}
