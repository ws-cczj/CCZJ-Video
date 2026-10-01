package service

import (
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
)

// ======================== Source ========================

func (a *App) GetAllSources() ([]*model.Source, error) {
	return handler.GetAllSources()
}

func (a *App) GetSourceStats() ([]model.SourceStat, error) {
	return handler.GetSourceStats()
}

func (a *App) AddSource(s *model.Source) error {
	return handler.AddSource(s)
}

func (a *App) UpdateSource(s *model.Source) error {
	return handler.UpdateSource(s)
}

func (a *App) DeleteSource(key string) error {
	return handler.DeleteSource(key)
}
