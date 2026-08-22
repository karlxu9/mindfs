package usecase

import (
	"context"
	"errors"
	"io"

	"mindfs/server/internal/backup"
)

// backupExporter is satisfied by the real AppContext only; an optional
// interface so test registries do not have to implement export (same pattern
// as rootScanner).
type backupExporter interface {
	ExportBackup(ctx context.Context, w io.Writer, input backup.ExportInput) (backup.Manifest, error)
}

func (s *Service) ExportBackup(ctx context.Context, w io.Writer, input backup.ExportInput) (backup.Manifest, error) {
	if err := s.ensureRegistry(); err != nil {
		return backup.Manifest{}, err
	}
	exporter, ok := s.Registry.(backupExporter)
	if !ok {
		return backup.Manifest{}, errors.New("backup export not supported")
	}
	return exporter.ExportBackup(ctx, w, input)
}

// storageInspector is the optional interface for the storage checkup (R-5.3),
// satisfied by the real AppContext only.
type storageInspector interface {
	StorageReport(ctx context.Context, rootID string) (backup.StorageReport, error)
	StorageCleanup(ctx context.Context, rootID string) (backup.CleanupResult, error)
}

func (s *Service) StorageReport(ctx context.Context, rootID string) (backup.StorageReport, error) {
	if err := s.ensureRegistry(); err != nil {
		return backup.StorageReport{}, err
	}
	inspector, ok := s.Registry.(storageInspector)
	if !ok {
		return backup.StorageReport{}, errors.New("storage report not supported")
	}
	return inspector.StorageReport(ctx, rootID)
}

func (s *Service) StorageCleanup(ctx context.Context, rootID string) (backup.CleanupResult, error) {
	if err := s.ensureRegistry(); err != nil {
		return backup.CleanupResult{}, err
	}
	inspector, ok := s.Registry.(storageInspector)
	if !ok {
		return backup.CleanupResult{}, errors.New("storage cleanup not supported")
	}
	return inspector.StorageCleanup(ctx, rootID)
}
