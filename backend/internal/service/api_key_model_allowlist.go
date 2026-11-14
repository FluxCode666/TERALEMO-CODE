package service

import "context"

func (s *APIKeyService) SetModelAllowlistSettings(settings *SettingService) {
	s.modelAllowlistSettings = settings
	if s.cache != nil {
		settings.modelAllowlistPublisher = s.cache.PublishAuthCacheInvalidation
		if observations, ok := s.cache.(ModelAllowlistObservationStore); ok {
			settings.modelAllowlistObservations = observations
		}
	}
}

func (s *APIKeyService) CheckModelRequestIP(ctx context.Context, clientIP string, key *APIKey) (bool, error) {
	if s.modelAllowlistSettings == nil {
		return true, nil
	}
	settings, err := s.modelAllowlistSettings.GetModelAllowlist(ctx)
	if err != nil {
		return false, err
	}
	matched := s.modelAllowlistSettings.ModelAllowlistMatches(ctx, settings, clientIP)
	if !matched && key != nil && key.User != nil {
		s.modelAllowlistSettings.RecordModelAllowlistObservation(ctx, clientIP, key)
	}
	return !settings.Enabled || matched, nil
}
