package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/diagnosticrecording"
)

// GetDiagnosticsRecordingStatus reads only the small current-session state.
func (a *App) GetDiagnosticsRecordingStatus() diagnosticrecording.Status {
	if a.recording == nil {
		return diagnosticrecording.Status{}
	}
	status := a.recording.Status()
	if status.Profile.Mode != a.currentMode() {
		status.Profile = diagnosticrecording.ProfileStatus{}
	}
	if status.Mode != "" && status.Mode != a.currentMode() {
		return diagnosticrecording.Status{Profile: status.Profile}
	}
	return status
}

// StartDiagnosticsRecording starts a bounded, low-frequency reproduction session.
func (a *App) StartDiagnosticsRecording() (diagnosticrecording.Status, error) {
	if a.recording == nil {
		return diagnosticrecording.Status{}, apperror.New(apperror.Internal, "诊断记录器尚未初始化")
	}
	a.modeSwitchMu.Lock()
	defer a.modeSwitchMu.Unlock()
	status, err := a.recording.StartMode(a.currentMode())
	if err != nil {
		return status, apperror.Wrap(apperror.Storage, err, "开始诊断记录失败")
	}
	return status, nil
}

// StopDiagnosticsRecording drains accepted events and closes the timeline.
func (a *App) StopDiagnosticsRecording() (diagnosticrecording.Status, error) {
	if a.recording == nil {
		return diagnosticrecording.Status{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, err := a.recording.Stop(ctx)
	if err != nil {
		return status, apperror.Wrap(apperror.Storage, err, "结束诊断记录失败")
	}
	return status, nil
}

type DiagnosticsReceipt struct {
	Accepted     int                        `json:"accepted"`
	Rejected     int                        `json:"rejected"`
	DailyEnabled bool                       `json:"daily_enabled"`
	Recording    diagnosticrecording.Status `json:"recording"`
}

// SubmitDiagnosticsEvents accepts metadata batches without waiting for disk writes.
func (a *App) SubmitDiagnosticsEvents(payload string) (DiagnosticsReceipt, error) {
	var receipt DiagnosticsReceipt
	if len(payload) > diagnosticrecording.MaxBatchBytes {
		return receipt, apperror.New(apperror.Validation, "诊断批次超过 64 KiB")
	}
	var events []diagnosticrecording.Event
	if err := json.Unmarshal([]byte(payload), &events); err != nil || len(events) > diagnosticrecording.MaxBatchEvents {
		return receipt, apperror.New(apperror.Validation, "诊断批次格式无效")
	}
	a.modeSwitchMu.RLock()
	defer a.modeSwitchMu.RUnlock()
	if a.recording == nil {
		return receipt, apperror.New(apperror.Internal, "诊断记录器尚未初始化")
	}
	for _, event := range events {
		if event.Mode == a.currentMode() && a.recording.Record(event) {
			receipt.Accepted++
		} else {
			receipt.Rejected++
		}
	}
	receipt.DailyEnabled = a.recording.DailyEnabled()
	receipt.Recording = a.recording.Status()
	return receipt, nil
}

// GetDiagnosticsSession returns the current mode's last 200 metadata events.
func (a *App) GetDiagnosticsSession() diagnosticrecording.Session {
	a.modeSwitchMu.RLock()
	defer a.modeSwitchMu.RUnlock()
	if a.recording == nil {
		return diagnosticrecording.Session{}
	}
	return a.recording.Session(a.currentMode(), 200)
}

// CaptureDiagnosticsProfile is separate from routine collection and report export.
func (a *App) CaptureDiagnosticsProfile(kind string) (diagnosticrecording.ProfileStatus, error) {
	a.modeSwitchMu.RLock()
	defer a.modeSwitchMu.RUnlock()
	if a.recording == nil {
		return diagnosticrecording.ProfileStatus{}, apperror.New(apperror.Internal, "诊断记录器尚未初始化")
	}
	status, err := a.recording.CaptureProfile(kind, a.currentMode())
	if err != nil {
		return status, apperror.Wrap(apperror.Unavailable, err, "启动性能剖析失败")
	}
	return status, nil
}

// AddDiagnosticsRecordingMarker timestamps bounded frontend observations on the Go clock.
func (a *App) AddDiagnosticsRecordingMarker(kind, payload string) (bool, error) {
	a.modeSwitchMu.RLock()
	defer a.modeSwitchMu.RUnlock()
	if a.recording == nil {
		return false, nil
	}
	var envelope struct {
		Mode string `json:"application_mode"`
	}
	if json.Unmarshal([]byte(payload), &envelope) == nil && envelope.Mode != "" && envelope.Mode != a.currentMode() {
		return false, nil
	}
	if status := a.recording.Status(); status.Mode != "" && status.Mode != a.currentMode() {
		return false, nil
	}
	accepted, err := a.recording.Mark(kind, payload)
	if err != nil {
		code := apperror.Storage
		if errors.Is(err, diagnosticrecording.ErrInvalidMarker) {
			code = apperror.Validation
		}
		return false, apperror.Wrap(code, err, "写入诊断事件失败")
	}
	return accepted, nil
}
