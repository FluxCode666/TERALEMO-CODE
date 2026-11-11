package service

import (
	"context"
	"log/slog"
	"time"
)

type ModelAllowlistObservation struct {
	IP        string `json:"ip"`
	UserID    int64  `json:"user_id"`
	UserEmail string `json:"user_email"`
	KeyID     int64  `json:"key_id"`
	KeyName   string `json:"key_name"`
	Count     int64  `json:"count"`
	LastSeen  int64  `json:"last_seen"`
}

type ModelAllowlistObservationStore interface {
	RecordModelAllowlistObservation(context.Context, ModelAllowlistObservation) error
	ListModelAllowlistObservations(context.Context) ([]ModelAllowlistObservation, error)
}

func (s *SettingService) RecordModelAllowlistObservation(ctx context.Context, clientIP string, key *APIKey) {
	if s.modelAllowlistObservations == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	err := s.modelAllowlistObservations.RecordModelAllowlistObservation(ctx, ModelAllowlistObservation{
		IP: clientIP, UserID: key.User.ID, UserEmail: key.User.Email, KeyID: key.ID, KeyName: key.Name,
		LastSeen: time.Now().Unix(),
	})
	if err != nil {
		slog.Warn("record model allowlist observation failed", "error", err)
	}
}

func (s *SettingService) ListModelAllowlistObservations(ctx context.Context) ([]ModelAllowlistObservation, error) {
	if s.modelAllowlistObservations == nil {
		return []ModelAllowlistObservation{}, nil
	}
	return s.modelAllowlistObservations.ListModelAllowlistObservations(ctx)
}
