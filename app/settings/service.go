// Package settings provides the application settings boundary.
package settings

import "cczjVideo/app/db"

// Service owns access to persisted application settings.
type Service struct{}

// NewService creates a settings service backed by the application database.
func NewService() *Service {
	return &Service{}
}

// Get returns a persisted setting value.
func (s *Service) Get(key string) (string, error) {
	return db.GetSetting(key)
}

// Set persists a setting value.
func (s *Service) Set(key, value string) error {
	return db.SetSetting(key, value)
}
