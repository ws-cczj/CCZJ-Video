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
	if status.Mode != "" && status.Mode != a.currentMode() {
		return diagnosticrecording.Status{}
	}
	return status
}

// StartDiagnosticsRecording enables background sampling until stopped or the app exits.
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

// StopDiagnosticsRecording closes the timeline and captures final Go profiles.
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
