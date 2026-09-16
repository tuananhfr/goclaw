package mediaworker

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type assembleProcessor interface {
	Process(ctx context.Context, jobID string, manifest Manifest) (Result, error)
}

type Server struct {
	processor assembleProcessor
	root      string
	token     string
	secret    []byte
	baseURL   string
	slots     chan struct{}
}

type AssembleRequest struct {
	JobID    string   `json:"job_id"`
	Manifest Manifest `json:"manifest"`
}

type AssembleResponse struct {
	ContractVersion int    `json:"contract_version"`
	DownloadURL     string `json:"download_url"`
	Result
}

func NewServer(processor assembleProcessor, root, token, baseURL string, maxConcurrent int) (*Server, error) {
	if processor == nil || strings.TrimSpace(root) == "" || len(strings.TrimSpace(token)) < 32 {
		return nil, errors.New("processor, output root, and a 32-character bearer token are required")
	}
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("create output signing secret: %w", err)
	}
	return &Server{processor: processor, root: root, token: token, secret: secret, baseURL: strings.TrimRight(baseURL, "/"), slots: make(chan struct{}, maxConcurrent)}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "contract_version": ContractVersion})
	})
	mux.HandleFunc("POST /v1/assemble", s.authorize(s.handleAssemble))
	mux.HandleFunc("GET /v1/outputs/{job_id}", s.handleOutput)
	return mux
}

func (s *Server) handleAssemble(w http.ResponseWriter, r *http.Request) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "media worker concurrency limit reached"})
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	var request AssembleRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid assemble request"})
		return
	}
	result, err := s.processor.Process(r.Context(), request.JobID, request.Manifest)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	checksum, err := fileSHA256(result.Path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not checksum output"})
		return
	}
	result.Checksum = checksum
	baseURL := s.baseURL
	if baseURL == "" {
		baseURL = requestBaseURL(r)
	}
	downloadURL := fmt.Sprintf("%s/v1/outputs/%s?token=%s", baseURL, request.JobID, s.outputToken(request.JobID))
	writeJSON(w, http.StatusOK, AssembleResponse{ContractVersion: ContractVersion, DownloadURL: downloadURL, Result: result})
}

func (s *Server) handleOutput(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(r.PathValue("job_id"))
	actual := r.URL.Query().Get("token")
	expected := s.outputToken(jobID)
	if len(actual) != len(expected) || subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.root, jobID, "output.mp4")
	if filepath.Clean(path) != path || !strings.HasPrefix(path, filepath.Clean(s.root)+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actual := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(actual) != len(s.token) || subtle.ConstantTimeCompare([]byte(actual), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) outputToken(jobID string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte("media-output|" + jobID))
	return hex.EncodeToString(mac.Sum(nil))
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host
}
