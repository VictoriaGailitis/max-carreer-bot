package maxbot

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultAPIBase = "https://platform-api2.max.ru"

// NewHTTPClient loads an optional trusted CA in addition to the system roots.
// TLS verification remains enabled, including hostname validation.
func NewHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
		pemBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		if !roots.AppendCertsFromPEM(pemBytes) {
			return nil, errors.New("MAX CA file has no valid certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Timeout: 5 * time.Second, Transport: transport}, nil
}

type SendResult struct {
	Retryable  bool
	RetryAfter time.Duration
}

type Sender interface {
	Send(context.Context, int64) (SendResult, error)
}

type Client struct {
	http   *http.Client
	base   string
	token  string
	webApp string
}

func NewClient(token, base, webApp string, httpClient *http.Client) (*Client, error) {
	if token == "" || strings.ContainsAny(token, "\r\n") || webApp == "" {
		return nil, errors.New("invalid MAX client configuration")
	}
	if base == "" {
		base = DefaultAPIBase
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("MAX API base must be an HTTPS origin")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{http: httpClient, base: strings.TrimSuffix(base, "/"), token: token, webApp: webApp}, nil
}

func (c *Client) Send(ctx context.Context, recipient int64) (SendResult, error) {
	if recipient <= 0 {
		return SendResult{}, errors.New("invalid recipient")
	}
	message := struct {
		Text        string `json:"text"`
		Attachments []any  `json:"attachments"`
	}{Text: "Привет! В навигаторе можно подобрать образовательные возможности и сохранить интересные варианты. Откройте приложение, чтобы начать.",
		Attachments: []any{map[string]any{"type": "inline_keyboard", "payload": map[string]any{"buttons": [][]any{{map[string]string{
			"type": "open_app", "text": "Открыть навигатор", "web_app": c.webApp,
		}}}}}}}
	body, err := json.Marshal(message)
	if err != nil {
		return SendResult{}, err
	}
	endpoint := c.base + "/messages?user_id=" + strconv.FormatInt(recipient, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return SendResult{}, err
	}
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return SendResult{Retryable: true}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return SendResult{}, nil
	}
	result := SendResult{Retryable: resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500}
	if value := resp.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			result.RetryAfter = time.Duration(seconds) * time.Second
		} else if at, err := http.ParseTime(value); err == nil {
			result.RetryAfter = time.Until(at)
		}
	}
	return result, fmt.Errorf("MAX returned HTTP %d", resp.StatusCode)
}
