package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func sendReliableTrafficReport(ctx context.Context, batch trafficEnvelope) error {
	data, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	if httpAESCrypto != nil {
		encrypted, err := httpAESCrypto.Encrypt(data)
		if err != nil {
			return err
		}
		data, err = json.Marshal(map[string]any{"encrypted": true, "data": encrypted, "timestamp": time.Now().Unix()})
		if err != nil {
			return err
		}
	}
	urls := prioritizeURLs(strings.Split(httpReportURL, ","), loadPreferredURL(&preferredUploadURL))
	for _, target := range urls {
		parsed, err := url.Parse(target)
		if err != nil || parsed.Host == "" {
			continue
		}
		parsed.Path = "/flow/upload/v2"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := reportDo(ctx, req, 5*time.Second)
		if err != nil {
			continue
		}
		var ack struct {
			Stream   string `json:"stream"`
			Sequence int64  `json:"sequence"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&ack)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK && err == nil && ack.Stream == batch.Stream && ack.Sequence == batch.Sequence {
			storePreferredURL(&preferredUploadURL, target)
			return nil
		}
	}
	return errors.New("traffic batch not acknowledged; retained for retry (panel must support /flow/upload/v2)")
}
