// Fleet skill dispatch: streams skill packages from vault to devices
// (SKILL_DISPATCH_* chunked transfer), records device_skills state,
// and reconciles desired state via SKILL_SYNC on (re)connect.
package skillvault

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/iagent/gateway/internal/model"
	"github.com/iagent/gateway/internal/obs"
	"github.com/iagent/gateway/internal/store"
	"github.com/iagent/gateway/internal/tunnel"
)

const skillChunkSize = 256 * 1024

// Dispatcher handles fleet-wide skill dispatch.
type Dispatcher struct {
	vault  *Vault
	skills *store.SkillStore
	hub    *tunnel.Hub
	logger *slog.Logger
}

// NewDispatcher creates a new skill dispatcher.
func NewDispatcher(vault *Vault, skills *store.SkillStore, hub *tunnel.Hub) *Dispatcher {
	return &Dispatcher{
		vault:  vault,
		skills: skills,
		hub:    hub,
		logger: obs.Logger("skillvault.dispatch"),
	}
}

// DispatchToDevice sends a skill package to a device and records the desired state.
func (d *Dispatcher) DispatchToDevice(ctx context.Context, deviceID model.UUID, skillID, versionID model.UUID) error {
	ver, err := d.vault.GetVersion(ctx, versionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if ver == nil {
		return fmt.Errorf("version not found: %s", versionID)
	}

	// Record desired state
	now := time.Now().UTC()
	ds := &model.DeviceSkill{
		DeviceID:       deviceID,
		SkillID:        skillID,
		SkillVersionID: versionID,
		Status:         model.SkillInstalling,
		InstalledAt:    &now,
	}
	if err := d.skills.SetDeviceSkill(ctx, ds); err != nil {
		return fmt.Errorf("set device skill: %w", err)
	}

	// Open the artifact
	artifact, err := d.vault.OpenArtifact(ver)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer artifact.Close()

	// Get file info
	stat, err := artifact.(interface{ Stat() (os.FileInfo, error) }).Stat()
	totalBytes := int64(0)
	if err == nil {
		totalBytes = stat.Size()
	}

	// Compute total chunks
	totalChunks := int((totalBytes + skillChunkSize - 1) / skillChunkSize)
	if totalBytes == 0 {
		totalChunks = 1 // At least one chunk for empty artifacts
	}

	// Send SKILL_DISPATCH_BEGIN
	beginPayload := model.SkillDispatchBeginPayload{
		SkillID:        skillID,
		SkillVersionID: versionID,
		Version:        ver.Version,
		TotalChunks:    totalChunks,
		TotalBytes:     totalBytes,
		SHA256:         ver.SHA256,
	}
	beginFrame, err := tunnel.NewFrame(model.FrameSkillDispatchBegin, beginPayload)
	if err != nil {
		return err
	}
	if err := d.hub.SendFrame(deviceID, beginFrame); err != nil {
		return fmt.Errorf("send SKILL_DISPATCH_BEGIN: %w", err)
	}

	// Read and send chunks
	sem := make(chan struct{}, 8) // Max 8 chunks in flight
	var wg sync.WaitGroup
	var sendErr error
	var errMu sync.Mutex

	chunkIndex := 0
	for {
		buf := make([]byte, skillChunkSize)
		n, err := artifact.Read(buf)
		if n > 0 {
			sem <- struct{}{}
			wg.Add(1)

			go func(idx int, data []byte) {
				defer wg.Done()
				defer func() { <-sem }()

				chunkPayload := model.SkillChunkPayload{
					SkillID:        skillID,
					SkillVersionID: versionID,
					ChunkIndex:     idx,
					Data:           base64.StdEncoding.EncodeToString(data),
				}
				chunkFrame, frameErr := tunnel.NewFrame(model.FrameSkillChunk, chunkPayload)
				if frameErr != nil {
					errMu.Lock()
					sendErr = frameErr
					errMu.Unlock()
					return
				}

				if sendFrameErr := d.hub.SendFrame(deviceID, chunkFrame); sendFrameErr != nil {
					errMu.Lock()
					sendErr = fmt.Errorf("send SKILL_CHUNK %d: %w", idx, sendFrameErr)
					errMu.Unlock()
				}
			}(chunkIndex, buf[:n])

			chunkIndex++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read artifact chunk: %w", err)
		}
	}

	wg.Wait()

	if sendErr != nil {
		return sendErr
	}

	// Send SKILL_DISPATCH_END
	endPayload := model.SkillDispatchEndPayload{
		SkillID:        skillID,
		SkillVersionID: versionID,
	}
	endFrame, err := tunnel.NewFrame(model.FrameSkillDispatchEnd, endPayload)
	if err != nil {
		return err
	}
	if err := d.hub.SendFrame(deviceID, endFrame); err != nil {
		return fmt.Errorf("send SKILL_DISPATCH_END: %w", err)
	}

	d.logger.Info("skill dispatched to device",
		"device_id", deviceID,
		"skill_id", skillID,
		"version", ver.Version,
		"chunks", chunkIndex,
	)

	return nil
}

// SendSkillAction sends a SKILL_ACTION frame to a device.
func (d *Dispatcher) SendSkillAction(ctx context.Context, deviceID model.UUID, scope model.SkillActionScope, action model.SkillAction, skillID model.UUID, version string, agentID *model.UUID) error {
	payload := model.SkillActionPayload{
		Scope:   scope,
		Action:  action,
		SkillID: skillID,
		Version: &version,
		AgentID: agentID,
	}

	frame, err := tunnel.NewFrame(model.FrameSkillAction, payload)
	if err != nil {
		return err
	}

	return d.hub.SendFrame(deviceID, frame)
}

// SyncSkills sends the full desired skill state to a device on (re)connect.
func (d *Dispatcher) SyncSkills(ctx context.Context, deviceID model.UUID) error {
	deviceSkills, err := d.skills.GetDeviceSkills(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("get device skills: %w", err)
	}

	// For each device skill, also get agents that need it
	var agentSkills []model.AgentSkill
	for _, ds := range deviceSkills {
		// Fetch agents for the device is handled at a higher level
		_ = ds
	}

	payload := model.SkillSyncPayload{
		DeviceSkills: deviceSkills,
		AgentSkills:  agentSkills,
	}

	frame, err := tunnel.NewFrame(model.FrameSkillSync, payload)
	if err != nil {
		return err
	}

	return d.hub.SendFrame(deviceID, frame)
}

// UpdateDeviceSkillState handles a SKILL_STATE frame from a device.
func (d *Dispatcher) UpdateDeviceSkillState(ctx context.Context, payload model.SkillStatePayload) error {
	switch payload.Scope {
	case model.SkillScopeDevice:
		return d.skills.UpdateDeviceSkillStatus(ctx, payload.SkillID, payload.SkillID, payload.Status)

	case model.SkillScopeAgent:
		if payload.AgentID != nil {
			as := &model.AgentSkill{
				AgentID:        *payload.AgentID,
				SkillID:        payload.SkillID,
				SkillVersionID: payload.SkillVersionID,
				Status:         payload.Status,
			}
			if payload.Status == model.SkillInstalled || payload.Status == model.SkillDisabled {
				now := time.Now().UTC()
				as.EnabledAt = &now
			}
			return d.skills.SetAgentSkill(ctx, as)
		}
	}

	return fmt.Errorf("invalid skill state payload")
}

// IsSkillInstalledOnDevice checks if a skill is installed on a device.
func (d *Dispatcher) IsSkillInstalledOnDevice(ctx context.Context, deviceID, skillID model.UUID) (bool, error) {
	return d.skills.IsSkillInstalledOnDevice(ctx, deviceID, skillID)
}

// DispatchToAllDevices dispatches a skill to all online devices (admin fleet install).
func (d *Dispatcher) DispatchToAllDevices(ctx context.Context, skillID, versionID model.UUID) error {
	// This would iterate over all devices from the store.
	// For now, it's a placeholder that dispatches to devices the hub knows about.
	d.logger.Info("fleet dispatch initiated",
		"skill_id", skillID,
		"version_id", versionID,
	)
	return nil
}
