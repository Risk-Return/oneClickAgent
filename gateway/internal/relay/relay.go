// Package relay handles file staging (local disk or S3) and
// chunked push over the tunnel (FILE_PUSH_BEGIN/CHUNK/END)
// with backpressure (≤8 chunks in flight) and sha256 integrity verification.
package relay

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/store"
	"github.com/oneClickAgent/gateway/internal/tunnel"
)

const (
	// ChunkSize is the raw chunk size for file transfer (256 KiB).
	ChunkSize = 256 * 1024
	// MaxChunksInFlight is the maximum number of concurrent chunks per file.
	MaxChunksInFlight = 8
)

// FileRelay handles file staging and tunnel push/pull.
type FileRelay struct {
	store     store.FileStoreInterface
	hub       *tunnel.Hub
	baseDir   string
	maxSize   int64
	retention time.Duration

	pullMu    sync.Mutex
	pullBufs  map[model.UUID]*pullTransfer
}

type pullTransfer struct {
	fileID   model.UUID
	jobID    model.UUID
	name     string
	sha256   string
	chunks   int
	buf      [][]byte
	received []bool
}

// NewFileRelay creates a new file relay service.
func NewFileRelay(fileStore store.FileStoreInterface, hub *tunnel.Hub, baseDir string, maxSize int64, retention time.Duration) *FileRelay {
	return &FileRelay{
		store:     fileStore,
		hub:       hub,
		baseDir:   baseDir,
		maxSize:   maxSize,
		retention: retention,
		pullBufs:  make(map[model.UUID]*pullTransfer),
	}
}

// StageFile saves an uploaded file to local staging and creates a DB record.
func (r *FileRelay) StageFile(ctx context.Context, userID model.UUID, fileName string, reader io.Reader, mimeType string) (*model.File, error) {
	fileID := model.NewUUID()
	storagePath := filepath.Join(r.baseDir, fileID.String())

	// Sanitize filename: keep only the base name, strip path separators
	safeName := filepath.Base(filepath.Clean(fileName))
	if safeName == "." || safeName == "/" {
		safeName = "upload"
	}

	// Ensure directory exists
	if err := os.MkdirAll(r.baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}

	f, err := os.Create(storagePath)
	if err != nil {
		return nil, fmt.Errorf("create staged file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	limitReader := io.LimitReader(reader, r.maxSize)
	tee := io.TeeReader(limitReader, hasher)

	written, err := io.Copy(f, tee)
	if err != nil {
		os.Remove(storagePath)
		return nil, fmt.Errorf("write staged file: %w", err)
	}

	if written >= r.maxSize {
		os.Remove(storagePath)
		return nil, fmt.Errorf("file exceeds max upload size of %d bytes", r.maxSize)
	}

	sha256Sum := hex.EncodeToString(hasher.Sum(nil))

	file := &model.File{
		ID:         fileID,
		UserID:     userID,
		Name:       safeName,
		Size:       written,
		Mime:       mimeType,
		SHA256:     sha256Sum,
		Status:     model.FileStagedCloud,
		StorageURI: storagePath,
	}

	if err := r.store.Create(ctx, file); err != nil {
		os.Remove(storagePath)
		return nil, fmt.Errorf("create file record: %w", err)
	}

	return file, nil
}

// PushFile sends a staged file to a device over the tunnel.
func (r *FileRelay) PushFile(ctx context.Context, fileID, jobID, deviceID model.UUID) error {
	file, err := r.store.GetByID(ctx, fileID)
	if err != nil {
		return err
	}
	if file == nil {
		return fmt.Errorf("file not found: %s", fileID)
	}
	if file.Status == model.FileStagedDevice {
		return nil // Already on device
	}

	// Open the staged file
	f, err := os.Open(file.StorageURI)
	if err != nil {
		return fmt.Errorf("open staged file: %w", err)
	}
	defer f.Close()

	// Compute total chunks
	totalChunks := int((file.Size + ChunkSize - 1) / ChunkSize)

	// Send FILE_PUSH_BEGIN
	beginPayload := model.FilePushBeginPayload{
		FileID:      file.ID,
		JobID:       jobID,
		FileName:    file.Name,
		SizeBytes:   file.Size,
		TotalChunks: totalChunks,
		SHA256:      file.SHA256,
	}
	beginFrame, err := tunnel.NewFrame(model.FrameFilePushBegin, beginPayload)
	if err != nil {
		return err
	}
	if err := r.hub.SendFrame(deviceID, beginFrame); err != nil {
		return fmt.Errorf("send FILE_PUSH_BEGIN: %w", err)
	}

	// Send chunks with backpressure
	sem := make(chan struct{}, MaxChunksInFlight)
	var wg sync.WaitGroup
	var sendErr error
	var errMu sync.Mutex

	for i := 0; i < totalChunks; i++ {
		buf := make([]byte, ChunkSize)
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("read chunk %d: %w", i, err)
		}
		if n == 0 {
			break
		}

		sem <- struct{}{}
		wg.Add(1)

		go func(chunkIndex int, data []byte) {
			defer wg.Done()
			defer func() { <-sem }()

			chunkPayload := model.FileChunkPayload{
				FileID:     fileID,
				ChunkIndex: chunkIndex,
				Data:       base64.StdEncoding.EncodeToString(data),
			}
			chunkFrame, err := tunnel.NewFrame(model.FrameFileChunk, chunkPayload)
			if err != nil {
				errMu.Lock()
				sendErr = err
				errMu.Unlock()
				return
			}

			if err := r.hub.SendFrame(deviceID, chunkFrame); err != nil {
				errMu.Lock()
				sendErr = fmt.Errorf("send chunk %d: %w", chunkIndex, err)
				errMu.Unlock()
			}
		}(i, buf[:n])
	}

	wg.Wait()

	if sendErr != nil {
		return sendErr
	}

	// Send FILE_PUSH_END
	endPayload := model.FilePushEndPayload{FileID: fileID}
	endFrame, err := tunnel.NewFrame(model.FrameFilePushEnd, endPayload)
	if err != nil {
		return err
	}
	if err := r.hub.SendFrame(deviceID, endFrame); err != nil {
		return fmt.Errorf("send FILE_PUSH_END: %w", err)
	}

	return nil
}

// OnFileAck handles a FILE_ACK frame from a device.
func (r *FileRelay) OnFileAck(ctx context.Context, payload model.FileAckPayload) error {
	if payload.Status == model.FileStagedDevice {
		return r.store.UpdateStatus(ctx, payload.FileID, model.FileStagedDevice)
	}
	errMsg := "unknown error"
	if payload.Error != nil {
		errMsg = *payload.Error
	}
	return fmt.Errorf("file ack error for %s: %s", payload.FileID, errMsg)
}

// CleanupStagedFiles removes staged files older than the retention period.
func (r *FileRelay) CleanupStagedFiles(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-r.retention)
	files, err := r.store.ListStagedCloud(ctx, cutoff)
	if err != nil {
		return err
	}

	for _, f := range files {
		if err := os.Remove(f.StorageURI); err != nil && !os.IsNotExist(err) {
			continue
		}
		_ = r.store.MarkPurged(ctx, f.ID)
	}

	return nil
}

// CleanupJobFiles marks all files associated with a job for purge.
func (r *FileRelay) CleanupJobFiles(ctx context.Context, jobID model.UUID) error {
	files, err := r.store.ListByJob(ctx, jobID)
	if err != nil {
		return err
	}

	for _, f := range files {
		_ = r.store.MarkPurged(ctx, f.ID)
		if f.StorageURI != "" {
			_ = os.Remove(f.StorageURI)
		}
	}

	return nil
}

// FileStoreBackend returns the configured base storage path.
func (r *FileRelay) FileStoreBackend() string {
	if strings.HasPrefix(r.baseDir, "s3://") {
		return "s3"
	}
	return "local"
}

// ─── Output File Pull (device → gateway) ─────────────────────

// OnFilePullBegin initializes a pull transfer buffer.
func (r *FileRelay) OnFilePullBegin(ctx context.Context, deviceID model.UUID, payload model.FilePullBeginPayload) error {
	r.pullMu.Lock()
	defer r.pullMu.Unlock()

	if payload.Size > r.maxSize {
		r.sendPullAck(deviceID, payload.FileID, "ERROR", "file too large")
		return fmt.Errorf("file too large: %d > %d", payload.Size, r.maxSize)
	}

	r.pullBufs[payload.FileID] = &pullTransfer{
		fileID:   payload.FileID,
		jobID:    payload.JobID,
		name:     payload.Name,
		sha256:   payload.SHA256,
		chunks:   payload.TotalChunks,
		buf:      make([][]byte, payload.TotalChunks),
		received: make([]bool, payload.TotalChunks),
	}
	return nil
}

// OnFilePullChunk stores a chunk of an in-progress pull transfer.
func (r *FileRelay) OnFilePullChunk(ctx context.Context, deviceID model.UUID, payload model.FilePullChunkPayload) error {
	r.pullMu.Lock()
	pt := r.pullBufs[payload.FileID]
	r.pullMu.Unlock()

	if pt == nil {
		r.sendPullAck(deviceID, payload.FileID, "ERROR", "no active pull for file")
		return fmt.Errorf("no active pull for file %s", payload.FileID)
	}

	data, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		r.sendPullAck(deviceID, payload.FileID, "ERROR", "invalid base64 data")
		return fmt.Errorf("invalid base64: %w", err)
	}

	if payload.ChunkIndex < 0 || payload.ChunkIndex >= pt.chunks {
		r.sendPullAck(deviceID, payload.FileID, "ERROR", "chunk index out of range")
		return fmt.Errorf("chunk index %d out of range [0,%d)", payload.ChunkIndex, pt.chunks)
	}

	pt.buf[payload.ChunkIndex] = data
	pt.received[payload.ChunkIndex] = true
	return nil
}

// OnFilePullEnd finalizes a pull transfer, verifies SHA256, and stores the file.
func (r *FileRelay) OnFilePullEnd(ctx context.Context, deviceID model.UUID, payload model.FilePullEndPayload) error {
	r.pullMu.Lock()
	pt := r.pullBufs[payload.FileID]
	delete(r.pullBufs, payload.FileID)
	r.pullMu.Unlock()

	if pt == nil {
		r.sendPullAck(deviceID, payload.FileID, "ERROR", "no active pull for file")
		return fmt.Errorf("no active pull for file %s", payload.FileID)
	}

	for i, ok := range pt.received {
		if !ok {
			r.sendPullAck(deviceID, payload.FileID, "ERROR", fmt.Sprintf("missing chunk %d", i))
			return fmt.Errorf("missing chunk %d for file %s", i, payload.FileID)
		}
	}

	var total int64
	hasher := sha256.New()
	for _, chunk := range pt.buf {
		total += int64(len(chunk))
		hasher.Write(chunk)
	}
	computedSHA := hex.EncodeToString(hasher.Sum(nil))

	if computedSHA != pt.sha256 {
		r.sendPullAck(deviceID, payload.FileID, "ERROR", "sha256 mismatch")
		return fmt.Errorf("sha256 mismatch for file %s", payload.FileID)
	}

	outputDir := filepath.Join(r.baseDir, "jobs", pt.jobID.String(), "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		r.sendPullAck(deviceID, payload.FileID, "ERROR", "failed to create output dir")
		return fmt.Errorf("create output dir: %w", err)
	}

	storagePath := filepath.Join(outputDir, pt.name)
	f, err := os.Create(storagePath)
	if err != nil {
		r.sendPullAck(deviceID, payload.FileID, "ERROR", "failed to create file")
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	for _, chunk := range pt.buf {
		if _, err := f.Write(chunk); err != nil {
			os.Remove(storagePath)
			r.sendPullAck(deviceID, payload.FileID, "ERROR", "failed to write file")
			return fmt.Errorf("write file: %w", err)
		}
	}

	file := &model.File{
		ID:         pt.fileID,
		Name:       pt.name,
		Size:       total,
		SHA256:     pt.sha256,
		Status:     model.FileStagedCloud,
		StorageURI: storagePath,
	}
	if err := r.store.Create(ctx, file); err != nil {
		// Non-fatal: file is on disk
	}

	r.sendPullAck(deviceID, payload.FileID, "RECEIVED", "")
	return nil
}

func (r *FileRelay) sendPullAck(deviceID model.UUID, fileID model.UUID, status string, errMsg string) {
	ack := model.FilePullAckPayload{
		FileID: fileID,
		Status: status,
		Error:  errMsg,
	}
	frame, err := tunnel.NewFrame(model.FrameFilePullAck, ack)
	if err != nil {
		return
	}
	_ = r.hub.SendFrame(deviceID, frame)
}
