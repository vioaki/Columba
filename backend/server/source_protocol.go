package server

import (
	"columba.local/backend/storage"
	"context"
	"errors"
	"strings"
)

func (s *Server) validateSourceProtocol(source *storage.ModelSource) error {
	source.Platform = strings.TrimSpace(source.Platform)
	if !storage.NativeProtocol(source.Platform) {
		return errors.New("select openai, responses, claude or gemini")
	}
	return nil
}
func (s *Server) saveSource(ctx context.Context, source storage.ModelSource) error {
	return s.saveSourceChanges(ctx, storage.SourceUpdate{Source: source, SyncManualModels: true})
}
func (s *Server) saveSourceChanges(ctx context.Context, update storage.SourceUpdate) error {
	if err := s.store.SaveSource(ctx, update); err != nil {
		return err
	}
	s.noteSourceSaved(update.Source.ID, update.Source.AutoFetchModels)
	return nil
}
