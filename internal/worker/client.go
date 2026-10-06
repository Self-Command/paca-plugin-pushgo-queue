// Package worker reconciles Paca tasks and delivers persistent reminders.
package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/Self-Command/paca-plugin-pushgo-queue/internal/buildinfo"
	"github.com/jackc/pgx/v5"
)

const PluginID = "com.selfcommand.pushgo-queue"
const Version = buildinfo.Version
const Schema = "plugin_data_com_selfcommand_pushgo_queue"

type Worker struct {
	DB                                     *pgx.Conn
	API, Key, Secret                       string
	HTTP                                   *http.Client
	PublicURL, GatewayToken, EncryptionKey string
}
type apiError struct{ Code int }

func (e apiError) Error() string { return fmt.Sprintf("Paca API HTTP %d", e.Code) }
func readSecret(name string) (string, error) {
	path := os.Getenv(name + "_FILE")
	if path == "" {
		return "", fmt.Errorf("%s_FILE required", name)
	}
	b, err := os.ReadFile(path)
	return strings.TrimSpace(string(b)), err
}
func New(ctx context.Context) (*Worker, error) {
	key, err := readSecret("PACA_API_KEY")
	if err != nil || key == "" {
		return nil, errors.New("PACA_API_KEY_FILE required")
	}
	secret, err := readSecret("WORKER_SECRET")
	if err != nil || len(secret) != 64 {
		return nil, errors.New("WORKER_SECRET_FILE required")
	}
	base := strings.TrimRight(os.Getenv("PACA_API_URL"), "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("valid fixed PACA_API_URL required")
	}
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return nil, err
	}
	cfg.RuntimeParams["search_path"] = Schema
	db, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	publicURL := strings.TrimRight(os.Getenv("PUBLIC_URL"), "/")
	public, err := url.Parse(publicURL)
	if err != nil || public.Scheme != "https" || public.Host == "" || public.User != nil {
		return nil, errors.New("HTTPS PUBLIC_URL required")
	}
	gatewayToken, err := readSecret("GATEWAY_TOKEN")
	if err != nil || gatewayToken == "" {
		return nil, errors.New("GATEWAY_TOKEN_FILE required")
	}
	encryption, err := readSecret("ENCRYPTION_KEY")
	if err != nil || len(encryption) != 64 {
		return nil, errors.New("ENCRYPTION_KEY_FILE required")
	}
	return &Worker{DB: db, API: base, Key: key, Secret: secret, HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, PublicURL: publicURL, GatewayToken: gatewayToken, EncryptionKey: encryption}, nil
}
func (w *Worker) control(ctx context.Context) error {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	nonce := hex.EncodeToString(b)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(w.Secret))
	mac.Write([]byte("GET\n/worker/control\n" + stamp + "\n" + nonce))
	req, err := http.NewRequestWithContext(ctx, "GET", w.API+"/api/v1/plugins/"+PluginID+"/worker/control", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Worker-Timestamp", stamp)
	req.Header.Set("X-Worker-Nonce", nonce)
	req.Header.Set("X-Worker-Signature", hex.EncodeToString(mac.Sum(nil)))
	r, err := w.HTTP.Do(req)
	if err != nil {
		return errors.New("host control unavailable")
	}
	defer r.Body.Close()
	var c struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Schema  int    `json:"schema_version"`
		Enabled bool   `json:"enabled"`
		Source  string `json:"source_sha"`
	}
	if r.StatusCode != 200 || json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&c) != nil || !c.Enabled || c.ID != PluginID || c.Version != Version || c.Schema != 3 || len(buildinfo.SourceSHA) != 40 || c.Source != buildinfo.SourceSHA {
		return errors.New("host disabled or worker version/schema mismatch")
	}
	return nil
}
func (w *Worker) call(ctx context.Context, method, path string, body any, out any) error {
	if err := w.control(ctx); err != nil {
		return err
	}
	var input io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.API+"/api/v1"+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", w.Key)
	req.Header.Set("Content-Type", "application/json")
	r, err := w.HTTP.Do(req)
	if err != nil {
		return errors.New("Paca API response unknown")
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return apiError{r.StatusCode}
	}
	if out == nil {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(r.Body, 8*1024*1024)).Decode(&envelope); err != nil {
		return errors.New("Paca API response unknown")
	}
	return json.Unmarshal(envelope.Data, out)
}
