package notification

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Store struct{ DB *sql.DB }

func (s *Store) CreateBatch(ctx context.Context, tenant, key string, in BatchInput) (Batch, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Batch{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", tenant+":"+key); err != nil {
		return Batch{}, false, err
	}
	var id, hash string
	err = tx.QueryRowContext(ctx, "SELECT id,request_hash FROM valto_batches WHERE tenant_id=$1 AND request_key=$2", tenant, key).Scan(&id, &hash)
	if err == nil {
		if hash != inputHash(in) {
			return Batch{}, false, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return Batch{}, false, err
		}
		b, e := s.GetBatch(ctx, tenant, id)
		return b, true, e
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Batch{}, false, err
	}
	id = newID()
	if _, err = tx.ExecContext(ctx, "INSERT INTO valto_batches(id,tenant_id,request_key,request_hash,source) VALUES($1,$2,$3,$4,$5)", id, tenant, key, inputHash(in), in.Source); err != nil {
		return Batch{}, false, err
	}
	when := time.Now().UTC()
	if in.SendAt != nil {
		when = *in.SendAt
	}
	suppressed := 0
	for _, m := range in.Messages {
		r, e := tx.ExecContext(ctx, `INSERT INTO valto_messages(id,tenant_id,batch_id,external_id,dedup_key,recipient,body,status,scheduled_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,'queued',$8,$9) ON CONFLICT(tenant_id,dedup_key) DO NOTHING`, newID(), tenant, id, m.ExternalID, m.DedupKey, m.Recipient, m.Text, when, in.ExpiresAt)
		if e != nil {
			return Batch{}, false, e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return Batch{}, false, e
		}
		if n == 0 {
			suppressed++
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE valto_batches SET suppressed=$1 WHERE id=$2", suppressed, id); err != nil {
		return Batch{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Batch{}, false, err
	}
	b, e := s.GetBatch(ctx, tenant, id)
	return b, false, e
}

func (s *Store) GetBatch(ctx context.Context, tenant, id string) (Batch, error) {
	var b Batch
	err := s.DB.QueryRowContext(ctx, "SELECT id,source,state,created_at,suppressed FROM valto_batches WHERE tenant_id=$1 AND id=$2", tenant, id).Scan(&b.ID, &b.Source, &b.State, &b.CreatedAt, &b.Suppressed)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	err = s.DB.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status='queued'),count(*) FILTER(WHERE status='sending'),count(*) FILTER(WHERE status='accepted'),count(*) FILTER(WHERE status='rejected'),count(*) FILTER(WHERE status='unknown'),count(*) FILTER(WHERE status='canceled'),count(*) FILTER(WHERE status='expired') FROM valto_messages WHERE tenant_id=$1 AND batch_id=$2`, tenant, id).Scan(&b.Counts.Queued, &b.Counts.Sending, &b.Counts.Accepted, &b.Counts.Rejected, &b.Counts.Unknown, &b.Counts.Canceled, &b.Counts.Expired)
	return b, err
}

const messageColumns = "m.id,m.batch_id,m.external_id,m.recipient,m.body,m.status,m.scheduled_at,m.error_code,m.error_message,m.provider_id,m.attempt_count,m.retryable,m.created_at,m.updated_at"

type scanner interface{ Scan(...any) error }

func scanMessage(row scanner) (Message, error) {
	var m Message
	err := row.Scan(&m.ID, &m.BatchID, &m.ExternalID, &m.Recipient, &m.Text, &m.Status, &m.ScheduledAt, &m.ErrorCode, &m.ErrorMessage, &m.ProviderID, &m.AttemptCount, &m.Retryable, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return m, err
}
func (s *Store) GetMessage(ctx context.Context, tenant, id string) (Message, error) {
	return scanMessage(s.DB.QueryRowContext(ctx, "SELECT "+messageColumns+" FROM valto_messages m WHERE m.tenant_id=$1 AND m.id=$2", tenant, id))
}
func (s *Store) ListMessages(ctx context.Context, tenant, batch string, page int) ([]Message, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+messageColumns+" FROM valto_messages m WHERE m.tenant_id=$1 AND m.batch_id=$2 ORDER BY m.created_at,m.id LIMIT 50 OFFSET $3", tenant, batch, (page-1)*50)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Message{}
	for rows.Next() {
		m, e := scanMessage(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, m)
	}
	return items, rows.Err()
}
func (s *Store) Attempts(ctx context.Context, tenant, message string) ([]Attempt, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,message_id,status,code,reason,provider_id,started_at,finished_at FROM valto_attempts WHERE tenant_id=$1 AND message_id=$2 ORDER BY started_at,id", tenant, message)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Attempt{}
	for rows.Next() {
		var a Attempt
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Status, &a.Code, &a.Reason, &a.ProviderID, &a.StartedAt, &a.FinishedAt); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s *Store) ChangeBatch(ctx context.Context, tenant, id, state string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, "UPDATE valto_batches SET state=$1 WHERE tenant_id=$2 AND id=$3 AND state<>'canceled'", state, tenant, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	if state == "canceled" {
		if _, err = tx.ExecContext(ctx, "UPDATE valto_messages SET status='canceled',updated_at=NOW() WHERE tenant_id=$1 AND batch_id=$2 AND status='queued'", tenant, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Retry(ctx context.Context, tenant, id string) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE valto_messages m SET status='queued',scheduled_at=NOW(),retryable=FALSE,updated_at=NOW() FROM valto_batches b WHERE m.batch_id=b.id AND m.tenant_id=$1 AND m.id=$2 AND m.status='rejected' AND m.retryable AND b.state<>'canceled' AND (m.expires_at IS NULL OR m.expires_at>NOW())`, tenant, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) HasDue(ctx context.Context, tenant string) (bool, error) {
	var found bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM valto_messages m JOIN valto_batches b ON b.id=m.batch_id WHERE m.tenant_id=$1 AND m.status='queued' AND b.state='active' AND m.scheduled_at<=NOW() AND (m.expires_at IS NULL OR m.expires_at>NOW()))`, tenant).Scan(&found)
	return found, err
}

func (s *Store) Claim(ctx context.Context, tenant, lane string, interval time.Duration) (*Claim, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO valto_delivery_lanes(id) VALUES($1) ON CONFLICT DO NOTHING", lane); err != nil {
		return nil, err
	}
	var laneID string
	err = tx.QueryRowContext(ctx, "SELECT id FROM valto_delivery_lanes WHERE id=$1 AND next_send_at<=NOW() FOR UPDATE SKIP LOCKED", lane).Scan(&laneID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := scanMessage(tx.QueryRowContext(ctx, "SELECT "+messageColumns+` FROM valto_messages m JOIN valto_batches b ON b.id=m.batch_id WHERE m.tenant_id=$1 AND m.status='queued' AND b.state='active' AND m.scheduled_at<=NOW() AND (m.expires_at IS NULL OR m.expires_at>NOW()) ORDER BY m.scheduled_at,m.id LIMIT 1 FOR UPDATE OF m SKIP LOCKED`, tenant))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	aid := newID()
	if _, err = tx.ExecContext(ctx, "UPDATE valto_messages SET status='sending',attempt_count=attempt_count+1,current_attempt=$1,lease_until=NOW()+INTERVAL '60 seconds',updated_at=NOW() WHERE id=$2", aid, m.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO valto_attempts(id,tenant_id,message_id,status) VALUES($1,$2,$3,'sending')", aid, tenant, m.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE valto_delivery_lanes SET next_send_at=NOW()+($1 * INTERVAL '1 millisecond') WHERE id=$2", interval.Milliseconds(), lane); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	m.Status = "sending"
	m.AttemptCount++
	return &Claim{Message: m, AttemptID: aid}, nil
}

func (s *Store) Finish(ctx context.Context, tenant string, c Claim, d Delivery, maxAttempts int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state := d.Status
	when := time.Now().UTC()
	if d.Code == "http_429" && c.Message.AttemptCount < maxAttempts {
		state = "queued"
		delay := time.Duration(1<<uint(c.Message.AttemptCount)) * 10 * time.Second
		if d.RetryAfter > delay {
			delay = d.RetryAfter
		}
		if delay > time.Hour {
			delay = time.Hour
		}
		when = when.Add(delay)
	}
	r, err := tx.ExecContext(ctx, `UPDATE valto_messages SET status=$1,error_code=$2,error_message=$3,provider_id=$4,retryable=$5,scheduled_at=$6,lease_until=NULL,updated_at=NOW() WHERE tenant_id=$7 AND id=$8 AND current_attempt=$9 AND status='sending'`, state, d.Code, d.Reason, d.ProviderID, d.Retryable, when, tenant, c.Message.ID, c.AttemptID)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, "UPDATE valto_attempts SET status=$1,code=$2,reason=$3,provider_id=$4,finished_at=NOW() WHERE tenant_id=$5 AND id=$6", d.Status, d.Code, d.Reason, d.ProviderID, tenant, c.AttemptID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Recover(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `WITH expired AS (UPDATE valto_messages SET status='unknown',error_code='worker_lease_expired',error_message='Worker stopped before result was saved; reconcile before retrying.',retryable=FALSE,lease_until=NULL,updated_at=NOW() WHERE status='sending' AND lease_until<NOW() RETURNING current_attempt) UPDATE valto_attempts SET status='unknown',code='worker_lease_expired',reason='Worker stopped before result was saved; reconcile before retrying.',finished_at=NOW() WHERE id IN(SELECT current_attempt FROM expired)`)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE valto_messages SET status='expired',updated_at=NOW() WHERE status='queued' AND expires_at<=NOW()")
	return err
}
