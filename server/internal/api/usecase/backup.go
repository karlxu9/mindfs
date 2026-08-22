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
