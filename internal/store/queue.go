package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func (s *Store) ListQueues() ([]domain.Queue, error) {
	rows, err := s.DB.Query(`SELECT id, name, alias, enabled, overflow_after, overflow_alias, max_wait_ms FROM queues ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []domain.Queue
	for rows.Next() {
		q, err := scanQueue(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, q)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.loadQueueKids(&out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) GetQueue(id int64) (domain.Queue, error) {
	var q domain.Queue
	row := s.DB.QueryRow(`SELECT id, name, alias, enabled, overflow_after, overflow_alias, max_wait_ms FROM queues WHERE id=?`, id)
	q, err := scanQueue(row)
	if err != nil {
		return q, err
	}
	err = s.loadQueueKids(&q)
	return q, err
}

func (s *Store) GetQueueByAlias(alias string) (domain.Queue, error) {
	alias = strings.TrimSpace(alias)
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM queues WHERE alias=?`, alias).Scan(&id)
	if err == sql.ErrNoRows {
		err = s.DB.QueryRow(`SELECT queue_id FROM queue_aliases WHERE alias=?`, alias).Scan(&id)
	}
	if err != nil {
		return domain.Queue{}, err
	}
	return s.GetQueue(id)
}

func scanQueue(r rowScanner) (domain.Queue, error) {
	var q domain.Queue
	var en int
	err := r.Scan(&q.ID, &q.Name, &q.Alias, &en, &q.OverflowAfter, &q.OverflowAlias, &q.MaxWaitMS)
	q.Enabled = en == 1
	return q, err
}

func (s *Store) loadQueueKids(q *domain.Queue) error {
	rows, err := s.DB.Query(`SELECT pos, model_alias, max_concurrent FROM queue_steps WHERE queue_id=? ORDER BY pos`, q.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var st domain.QueueStep
		if err := rows.Scan(&st.Pos, &st.ModelAlias, &st.MaxConcurrent); err != nil {
			rows.Close()
			return err
		}
		if st.MaxConcurrent <= 0 {
			st.MaxConcurrent = 1
		}
		q.Steps = append(q.Steps, st)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	arows, err := s.DB.Query(`SELECT alias FROM queue_aliases WHERE queue_id=? ORDER BY alias`, q.ID)
	if err != nil {
		return err
	}
	defer arows.Close()
	for arows.Next() {
		var a string
		if err := arows.Scan(&a); err != nil {
			return err
		}
		q.ExtraAliases = append(q.ExtraAliases, a)
	}
	return arows.Err()
}

func (s *Store) AliasTaken(alias string, exceptQueueID, exceptModelID int64) (bool, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return true, nil
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM models WHERE alias=? AND id!=?`, alias, exceptModelID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM queues WHERE alias=? AND id!=?`, alias, exceptQueueID).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM queue_aliases WHERE alias=? AND queue_id!=?`, alias, exceptQueueID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) SaveQueue(q domain.Queue) (int64, error) {
	q.Name = strings.TrimSpace(q.Name)
	q.Alias = strings.TrimSpace(q.Alias)
	if q.Name == "" || q.Alias == "" {
		return 0, fmt.Errorf("name and alias required")
	}
	taken, err := s.AliasTaken(q.Alias, q.ID, 0)
	if err != nil {
		return 0, err
	}
	if taken {
		return 0, fmt.Errorf("alias %s already used", q.Alias)
	}
	if q.OverflowAfter < 0 {
		q.OverflowAfter = 0
	}
	en := 0
	if q.Enabled {
		en = 1
	}
	if q.ID == 0 {
		res, err := s.DB.Exec(`INSERT INTO queues (name, alias, enabled, overflow_after, overflow_alias, max_wait_ms) VALUES (?,?,?,?,?,?)`,
			q.Name, q.Alias, en, q.OverflowAfter, strings.TrimSpace(q.OverflowAlias), q.MaxWaitMS)
		if err != nil {
			return 0, err
		}
		q.ID, _ = res.LastInsertId()
	} else {
		_, err := s.DB.Exec(`UPDATE queues SET name=?, alias=?, enabled=?, overflow_after=?, overflow_alias=?, max_wait_ms=? WHERE id=?`,
			q.Name, q.Alias, en, q.OverflowAfter, strings.TrimSpace(q.OverflowAlias), q.MaxWaitMS, q.ID)
		if err != nil {
			return 0, err
		}
	}
	if _, err := s.DB.Exec(`DELETE FROM queue_steps WHERE queue_id=?`, q.ID); err != nil {
		return 0, err
	}
	for i, st := range q.Steps {
		alias := strings.TrimSpace(st.ModelAlias)
		if alias == "" {
			continue
		}
		capn := st.MaxConcurrent
		if capn <= 0 {
			capn = 1
		}
		if _, err := s.DB.Exec(`INSERT INTO queue_steps (queue_id, pos, model_alias, max_concurrent) VALUES (?,?,?,?)`,
			q.ID, i, alias, capn); err != nil {
			return 0, err
		}
	}
	return q.ID, nil
}

func (s *Store) DeleteQueue(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM queues WHERE id=?`, id)
	return err
}

func (s *Store) AddQueueAlias(queueID int64, alias string) error {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return fmt.Errorf("alias required")
	}
	taken, err := s.AliasTaken(alias, queueID, 0)
	if err != nil {
		return err
	}
	if taken {
		return fmt.Errorf("alias %s already used", alias)
	}
	q, err := s.GetQueue(queueID)
	if err != nil {
		return err
	}
	if q.Alias == alias {
		return nil
	}
	_, err = s.DB.Exec(`INSERT INTO queue_aliases (alias, queue_id) VALUES (?,?)`, alias, queueID)
	return err
}

func (s *Store) DeleteQueueAlias(queueID int64, alias string) error {
	_, err := s.DB.Exec(`DELETE FROM queue_aliases WHERE queue_id=? AND alias=?`, queueID, alias)
	return err
}

func (s *Store) InsertQueueJob(j *domain.QueueJob) error {
	if j.ID == "" {
		return fmt.Errorf("job id required")
	}
	var seq int64
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM queue_jobs WHERE queue_id=?`, j.QueueID).Scan(&seq); err != nil {
		return err
	}
	j.Seq = seq
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now().UTC()
	}
	_, err := s.DB.Exec(`INSERT INTO queue_jobs (id, queue_id, seq, status, alias, assigned_model, assigned_backend, provider, key_prefix, path, body, bytes, error, preview, created_at, started_at, finished_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.QueueID, j.Seq, j.Status, j.Alias, j.AssignedModel, j.AssignedBackend, j.Provider, j.KeyPrefix, j.Path,
		j.Body, j.Bytes, j.Error, j.Preview, j.CreatedAt.Format(time.RFC3339Nano), nullTime(j.StartedAt), nullTime(j.FinishedAt))
	return err
}

func (s *Store) UpdateQueueJob(j domain.QueueJob) error {
	_, err := s.DB.Exec(`UPDATE queue_jobs SET status=?, assigned_model=?, assigned_backend=?, provider=?, error=?, body=?, bytes=?, started_at=?, finished_at=? WHERE id=?`,
		j.Status, j.AssignedModel, j.AssignedBackend, j.Provider, j.Error, j.Body, j.Bytes, nullTime(j.StartedAt), nullTime(j.FinishedAt), j.ID)
	return err
}

func (s *Store) GetQueueJob(id string) (domain.QueueJob, error) {
	row := s.DB.QueryRow(`SELECT id, queue_id, seq, status, alias, assigned_model, assigned_backend, provider, key_prefix, path, body, bytes, error, preview, created_at, started_at, finished_at FROM queue_jobs WHERE id=?`, id)
	return scanQueueJob(row)
}

func (s *Store) ListQueueJobs(queueID int64, limit int) ([]domain.QueueJob, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.DB.Query(`SELECT id, queue_id, seq, status, alias, assigned_model, assigned_backend, provider, key_prefix, path, body, bytes, error, preview, created_at, started_at, finished_at
FROM queue_jobs WHERE queue_id=? ORDER BY seq DESC LIMIT ?`, queueID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.QueueJob
	for rows.Next() {
		j, err := scanQueueJob(rows)
		if err != nil {
			return nil, err
		}
		j.Body = nil
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) WaitingStats() (jobs int, bytes int64, err error) {
	err = s.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(bytes),0) FROM queue_jobs WHERE status IN ('waiting','running')`).Scan(&jobs, &bytes)
	return
}

func (s *Store) OldestWaiting(n int) ([]domain.QueueJob, error) {
	rows, err := s.DB.Query(`SELECT id, queue_id, seq, status, alias, assigned_model, assigned_backend, provider, key_prefix, path, body, bytes, error, preview, created_at, started_at, finished_at
FROM queue_jobs WHERE status='waiting' ORDER BY seq ASC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.QueueJob
	for rows.Next() {
		j, err := scanQueueJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) ResetStaleQueueJobs() error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.Exec(`UPDATE queue_jobs SET status='dropped', error='шлюз перезапущен', body=NULL, finished_at=? WHERE status IN ('waiting','running')`, now)
	return err
}

func (s *Store) PurgeFinishedQueueJobs(keep int, maxAge time.Duration) error {
	if keep < 8 {
		keep = 8
	}
	cut := time.Now().UTC().Add(-maxAge).Format(time.RFC3339Nano)
	_, _ = s.DB.Exec(`DELETE FROM queue_jobs WHERE status NOT IN ('waiting','running') AND finished_at IS NOT NULL AND finished_at<?`, cut)
	rows, err := s.DB.Query(`SELECT DISTINCT queue_id FROM queue_jobs`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		var extra []string
		r2, err := s.DB.Query(`SELECT id FROM queue_jobs WHERE queue_id=? AND status NOT IN ('waiting','running') ORDER BY seq DESC`, id)
		if err != nil {
			return err
		}
		n := 0
		for r2.Next() {
			var jid string
			if err := r2.Scan(&jid); err != nil {
				r2.Close()
				return err
			}
			n++
			if n > keep {
				extra = append(extra, jid)
			}
		}
		r2.Close()
		for _, jid := range extra {
			_, _ = s.DB.Exec(`DELETE FROM queue_jobs WHERE id=?`, jid)
		}
	}
	return nil
}

func scanQueueJob(r rowScanner) (domain.QueueJob, error) {
	var j domain.QueueJob
	var created string
	var started, finished sql.NullString
	var body []byte
	err := r.Scan(&j.ID, &j.QueueID, &j.Seq, &j.Status, &j.Alias, &j.AssignedModel, &j.AssignedBackend, &j.Provider, &j.KeyPrefix, &j.Path, &body, &j.Bytes, &j.Error, &j.Preview, &created, &started, &finished)
	if err != nil {
		return j, err
	}
	j.Body = body
	j.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if j.CreatedAt.IsZero() {
		j.CreatedAt, _ = time.Parse(time.RFC3339, created)
	}
	if started.Valid {
		t, _ := time.Parse(time.RFC3339Nano, started.String)
		if t.IsZero() {
			t, _ = time.Parse(time.RFC3339, started.String)
		}
		j.StartedAt = &t
	}
	if finished.Valid {
		t, _ := time.Parse(time.RFC3339Nano, finished.String)
		if t.IsZero() {
			t, _ = time.Parse(time.RFC3339, finished.String)
		}
		j.FinishedAt = &t
	}
	return j, nil
}

func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
