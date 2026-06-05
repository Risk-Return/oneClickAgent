package relay_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/relay"
	"github.com/oneClickAgent/gateway/internal/store"
	"github.com/oneClickAgent/gateway/internal/tunnel"
)

func TestFilePullFullFlow(t *testing.T) {
	tmpDir := t.TempDir()
	hub := tunnel.NewHub(tunnel.HubConfig{})
	r := relay.NewFileRelay(store.NewMockFileStore(), hub, tmpDir, 100<<20, 24*time.Hour)

	deviceID := model.NewUUID()
	fileID := model.NewUUID()
	jobID := model.NewUUID()

	content := []byte("hello world output file content")
	hash := sha256.Sum256(content)
	sha256Str := hex.EncodeToString(hash[:])
	chunkData := base64.StdEncoding.EncodeToString(content)

	err := r.OnFilePullBegin(context.Background(), deviceID, model.FilePullBeginPayload{
		FileID:      fileID,
		JobID:       jobID,
		Name:        "result.txt",
		Size:        int64(len(content)),
		SHA256:      sha256Str,
		TotalChunks: 1,
	})
	if err != nil {
		t.Fatalf("pull begin: %v", err)
	}

	err = r.OnFilePullChunk(context.Background(), deviceID, model.FilePullChunkPayload{
		FileID:     fileID,
		ChunkIndex: 0,
		Data:       chunkData,
	})
	if err != nil {
		t.Fatalf("pull chunk: %v", err)
	}

	err = r.OnFilePullEnd(context.Background(), deviceID, model.FilePullEndPayload{FileID: fileID})
	if err != nil {
		t.Fatalf("pull end: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "jobs", jobID.String(), "output", "result.txt")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q want %q", string(data), string(content))
	}
}

func TestFilePullSHA256Mismatch(t *testing.T) {
	hub := tunnel.NewHub(tunnel.HubConfig{})
	r := relay.NewFileRelay(store.NewMockFileStore(), hub, t.TempDir(), 100<<20, 24*time.Hour)

	deviceID := model.NewUUID()
	fileID := model.NewUUID()

	content := []byte("real content")
	chunkData := base64.StdEncoding.EncodeToString(content)

	r.OnFilePullBegin(context.Background(), deviceID, model.FilePullBeginPayload{
		FileID:      fileID,
		JobID:       model.NewUUID(),
		Name:        "bad.txt",
		Size:        int64(len(content)),
		SHA256:      "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		TotalChunks: 1,
	})
	r.OnFilePullChunk(context.Background(), deviceID, model.FilePullChunkPayload{
		FileID:     fileID,
		ChunkIndex: 0,
		Data:       chunkData,
	})
	err := r.OnFilePullEnd(context.Background(), deviceID, model.FilePullEndPayload{FileID: fileID})
	if err == nil {
		t.Error("expected sha256 mismatch error")
	}
}

func TestFilePullMissingChunk(t *testing.T) {
	hub := tunnel.NewHub(tunnel.HubConfig{})
	r := relay.NewFileRelay(store.NewMockFileStore(), hub, t.TempDir(), 100<<20, 24*time.Hour)

	deviceID := model.NewUUID()
	fileID := model.NewUUID()

	r.OnFilePullBegin(context.Background(), deviceID, model.FilePullBeginPayload{
		FileID:      fileID,
		JobID:       model.NewUUID(),
		Name:        "multi.txt",
		Size:        200,
		SHA256:      "aa",
		TotalChunks: 2,
	})
	err := r.OnFilePullEnd(context.Background(), deviceID, model.FilePullEndPayload{FileID: fileID})
	if err == nil {
		t.Error("expected missing chunk error")
	}
}

func TestFilePullChunkIndexOutOfRange(t *testing.T) {
	hub := tunnel.NewHub(tunnel.HubConfig{})
	r := relay.NewFileRelay(store.NewMockFileStore(), hub, t.TempDir(), 100<<20, 24*time.Hour)

	deviceID := model.NewUUID()
	fileID := model.NewUUID()

	r.OnFilePullBegin(context.Background(), deviceID, model.FilePullBeginPayload{
		FileID:      fileID,
		JobID:       model.NewUUID(),
		Name:        "out.txt",
		Size:        10,
		SHA256:      "aa",
		TotalChunks: 1,
	})
	err := r.OnFilePullChunk(context.Background(), deviceID, model.FilePullChunkPayload{
		FileID:     fileID,
		ChunkIndex: 99,
		Data:       base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if err == nil {
		t.Error("expected out of range error")
	}
}

func TestFilePullNoActiveTransfer(t *testing.T) {
	hub := tunnel.NewHub(tunnel.HubConfig{})
	r := relay.NewFileRelay(store.NewMockFileStore(), hub, t.TempDir(), 100<<20, 24*time.Hour)

	err := r.OnFilePullChunk(context.Background(), model.NewUUID(), model.FilePullChunkPayload{
		FileID:     model.NewUUID(),
		ChunkIndex: 0,
		Data:       "AAAA",
	})
	if err == nil {
		t.Error("expected no active pull error")
	}
}
