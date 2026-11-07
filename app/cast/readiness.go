package cast

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/operation"
	"context"
	"time"
)

type playbackEvidence struct {
	accepted bool
	started  bool
	sampled  bool
	position float64
	at       time.Time
}

// Device command ownership serializes SetURI/Play/Seek/Stop without blocking
// status reads. Waiting for another command remains cancellable.
func (s *Service) ownCommand(ctx context.Context, id string) (renderer, func(), error) {
	s.mu.Lock()
	d, ok := s.devices[id]
	if ok && d.command == nil {
		d.command = make(chan struct{}, 1)
		s.devices[id] = d
	}
	s.mu.Unlock()
	if !ok {
		return renderer{}, nil, apperror.New(apperror.Unavailable, "设备已失效，请重新搜索")
	}
	select {
	case d.command <- struct{}{}:
	case <-ctx.Done():
		return renderer{}, nil, ctx.Err()
	}
	release := func() { <-d.command }
	current, err := s.device(id)
	if err != nil {
		release()
		return renderer{}, nil, err
	}
	return current, release, nil
}

func (s *Service) observeStatus(d renderer, status Status) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.devices[d.ID]
	if !ok || current.session != d.session || current.session == "" || !d.playback.accepted {
		return false
	}
	e := current.playback
	if e.sampled && status.HasPosition && status.Position-e.position < -2 {
		e.started = false
	}
	now := time.Now()
	active := status.State == "PLAYING" || status.State == "PAUSED_PLAYBACK"
	if !e.accepted || !active || status.MediaState != "matched" || (status.TransportStatus == "ERROR_OCCURRED" || status.RelayHealth == "failed" || status.RelayHealth == "revoked") {
		e.started = false
		e.sampled = false
	} else if !status.HasPosition {
		e.sampled = false
	} else if status.State == "PLAYING" {
		if e.sampled {
			elapsed := now.Sub(e.at).Seconds()
			advance := status.Position - e.position
			if advance < -2 {
				e.started = false
			}
			if elapsed >= 1 && elapsed <= 12 && advance >= .5 && advance <= elapsed*4+2 {
				e.started = true
			}
		}
		e.position = status.Position
		e.at = now
		e.sampled = true
	} else {
		e.sampled = false
	}
	current.playback = e
	s.devices[d.ID] = current
	return e.started && status.HasPosition && active && status.MediaState == "matched" && status.TransportStatus != "ERROR_OCCURRED"
}

func (s *Service) requireSeekReady(ctx context.Context, d renderer) error {
	status, err := s.StatusContext(ctx, d.ID)
	if err != nil {
		return err
	}
	ready := s.seekReady(d, status)
	if ready {
		return nil
	}
	err = apperror.New(apperror.Unavailable, "电视尚未起播或未提供可用进度，请稍后重试")
	traceFailure(ctx, "cast.seek.rejected", err, applog.Fields{"code": "CAST_NOT_READY", "has_position": status.HasPosition, "media_state": status.MediaState, "transport_state": status.State, "position_state": status.PositionState, "relay_health": status.RelayHealth, "confirmation_state": status.Confirmation})
	return err
}

func (s *Service) seekReady(d renderer, status Status) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.devices[d.ID]
	return ok && current.session == d.session && current.playback.accepted && current.playback.started && status.HasPosition && status.MediaState == "matched" && status.TransportStatus != "ERROR_OCCURRED" && (status.State == "PLAYING" || status.State == "PAUSED_PLAYBACK")
}

func (s *Service) accepted(ctx context.Context, d renderer) {
	s.mu.Lock()
	current, ok := s.devices[d.ID]
	if ok && current.session == d.session {
		current.playback = playbackEvidence{accepted: true}
		s.devices[d.ID] = current
	}
	s.mu.Unlock()
	operation.Stage(ctx, "cast.play.accepted", applog.Fields{"seek_ready": false})
}
func (s *Service) resetSeekSample(d renderer, stop bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.devices[d.ID]
	if !ok || current.session != d.session {
		return
	}
	if stop {
		current.playback = playbackEvidence{}
	} else {
		current.playback.sampled = false
	}
	s.devices[d.ID] = current
}

func (s *Service) SessionID(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.devices[id].session
}
