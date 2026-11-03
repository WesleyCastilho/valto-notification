package notification

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed schema.sql
var schema string

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(75190261)"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO valto_schema_versions(version) VALUES('001') ON CONFLICT DO NOTHING"); err != nil {
		return err
	}
	return tx.Commit()
}
