package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
	"log"
	"net/http"
	"time"
)

type flowUploadEnvelope struct {
	Stream   string     `json:"stream"`
	Sequence int64      `json:"sequence"`
	Items    []flowItem `json:"items"`
}

var errInvalidTraffic = errors.New("invalid traffic upload")

func (h *Handler) serveFlowUpload(w http.ResponseWriter, r *http.Request, reliable bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	secret := r.URL.Query().Get("secret")
	node, err := h.repo.GetNodeBySecret(secret)
	if err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if node == nil {
		http.Error(w, "invalid node", http.StatusUnauthorized)
		return
	}
	raw, err := readAndDecryptFlowBody(r.Body, secret)
	if err != nil {
		http.Error(w, "invalid traffic body", http.StatusBadRequest)
		return
	}
	var envelope flowUploadEnvelope
	if reliable {
		err = json.Unmarshal([]byte(raw), &envelope)
		decoded, decodeErr := hex.DecodeString(envelope.Stream)
		if decodeErr != nil || len(decoded) != 16 || envelope.Sequence <= 0 {
			err = errInvalidTraffic
		}
	} else {
		err = json.Unmarshal([]byte(raw), &envelope.Items)
	}
	if err != nil {
		http.Error(w, "invalid traffic upload", http.StatusBadRequest)
		return
	}
	reject := func(status int, reason string) {
		if !reliable {
			http.Error(w, "invalid traffic upload", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"stream": envelope.Stream, "sequence": envelope.Sequence, "error": reason})
	}
	if validateFlowUploadItems(envelope.Items, nil) != nil {
		reject(http.StatusBadRequest, "invalid_batch")
		return
	}
	now := time.Now()
	var batch flowUploadBatch
	var quotas map[int64]*model.UserQuotaView
	apply := func(tx *repo.Repository) error {
		local := &Handler{repo: tx}
		metas, err := tx.GetFlowUploadForwardMetas(collectFlowUploadForwardIDs(envelope.Items))
		if err != nil {
			return err
		}
		if err := validateFlowUploadItems(envelope.Items, metas); err != nil {
			return fmt.Errorf("%w: %v", errInvalidTraffic, err)
		}
		batch = local.buildNodeFlowUploadBatch(node.ID, envelope.Items, metas)
		quotas, err = local.commitFlowUploadBatch(node.ID, &batch, now)
		if err != nil {
			return err
		}
		return local.recordTunnelMetricsFromForwardBatch(node.ID, batch.forwardTraffic, metas, now.UnixMilli())
	}
	applied := true
	if reliable {
		data, _ := json.Marshal(envelope.Items)
		applied, err = h.repo.ApplyTrafficBatch(fmt.Sprintf("flow:%d:%s", node.ID, envelope.Stream), envelope.Sequence, fmt.Sprintf("%x", sha256.Sum256(data)), apply)
	} else {
		err = h.repo.Transaction(apply)
	}
	if err != nil {
		log.Printf("flow upload failed node_id=%d err=%v", node.ID, err)
		var gap *repo.TrafficSequenceGap
		switch {
		case errors.As(err, &gap):
			reject(http.StatusConflict, "reset_stream")
		case errors.Is(err, repo.ErrTrafficSequence):
			reject(http.StatusConflict, "identity_conflict")
		case errors.Is(err, errInvalidTraffic):
			reject(http.StatusBadRequest, "invalid_batch")
		default:
			http.Error(w, "traffic upload not committed", http.StatusServiceUnavailable)
		}
		return
	}
	if reliable {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Stream   string `json:"stream"`
			Sequence int64  `json:"sequence"`
		}{envelope.Stream, envelope.Sequence})
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}
	// Commit acknowledgement must not wait for a slow/offline node control RPC.
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	if applied {
		h.enforceFlowUploadBatch(node.ID, batch, quotas)
	}
}
