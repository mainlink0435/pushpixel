package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusUploading Status = "uploading"
	StatusSuccess  Status = "success"
	StatusFailed   Status = "failed"
)

type TrackedFile struct {
	ID               int64     `json:"id"`
	AbsolutePath     string    `json:"absolute_path"`
	FileSize         int64     `json:"file_size"`
	ModTime          time.Time `json:"mod_time"`
	Status           Status    `json:"status"`
	GoogleMediaID    *string   `json:"google_media_id,omitempty"`
	ErrorMessage     *string   `json:"error_message,omitempty"`
	RetryCount       int       `json:"retry_count"`
	UploadedAt       *time.Time `json:"uploaded_at,omitempty"`
	LastCheckedAt    time.Time `json:"last_checked_at"`
	CreatedAt        time.Time `json:"created_at"`
}

type DB struct {
	db *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db := &DB{db: d}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func OpenTest(path string) (*DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db := &DB{db: d}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tracked_files (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		absolute_path   TEXT NOT NULL UNIQUE,
		file_size       INTEGER NOT NULL,
		mod_time        TEXT NOT NULL,
		status          TEXT NOT NULL DEFAULT 'pending'
			CHECK(status IN ('pending', 'uploading', 'success', 'failed')),
		google_media_id TEXT,
		error_message   TEXT,
		retry_count     INTEGER NOT NULL DEFAULT 0,
		uploaded_at     TEXT,
		last_checked_at TEXT NOT NULL DEFAULT (datetime('now')),
		created_at      TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE INDEX IF NOT EXISTS idx_tracked_files_status
		ON tracked_files(status);

	CREATE INDEX IF NOT EXISTS idx_tracked_files_google_media_id
		ON tracked_files(google_media_id);`

	if _, err := d.db.Exec(schema); err != nil {
		return err
	}

	var colCount int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('tracked_files') WHERE name = 'retry_count'`).Scan(&colCount)
	if err == nil && colCount == 0 {
		d.db.Exec(`ALTER TABLE tracked_files ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0`)
	}

	return nil
}

func (d *DB) UpsertFile(path string, fileSize int64, modTime time.Time) (*TrackedFile, error) {
	now := time.Now().UTC()
	modTime = modTime.Truncate(time.Second)
	modStr := modTime.UTC().Format(time.RFC3339)

	result, err := d.db.Exec(`
		INSERT INTO tracked_files (absolute_path, file_size, mod_time, status, last_checked_at)
		VALUES (?, ?, ?, 'pending', ?)
		ON CONFLICT(absolute_path) DO UPDATE SET
			file_size = excluded.file_size,
			mod_time = excluded.mod_time,
			last_checked_at = excluded.last_checked_at
	`, path, fileSize, modStr, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("upsert file: %w", err)
	}

	id, _ := result.LastInsertId()
	if id > 0 {
		return d.GetByID(id)
	}
	return d.GetByPath(path)
}

func (d *DB) GetByID(id int64) (*TrackedFile, error) {
	row := d.db.QueryRow(`
		SELECT id, absolute_path, file_size, mod_time, status,
			google_media_id, error_message, retry_count, uploaded_at, last_checked_at, created_at
		FROM tracked_files WHERE id = ?`, id)

	return d.scanFile(row)
}

func (d *DB) GetByPath(path string) (*TrackedFile, error) {
	row := d.db.QueryRow(`
		SELECT id, absolute_path, file_size, mod_time, status,
			google_media_id, error_message, retry_count, uploaded_at, last_checked_at, created_at
		FROM tracked_files WHERE absolute_path = ?`, path)

	return d.scanFile(row)
}

func (d *DB) TotalCount() (int, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM tracked_files").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (d *DB) CountByStatus(status Status) (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM tracked_files WHERE status = ?`, string(status)).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (d *DB) ListByStatus(status Status) ([]*TrackedFile, error) {
	rows, err := d.db.Query(`
		SELECT id, absolute_path, file_size, mod_time, status,
			google_media_id, error_message, retry_count, uploaded_at, last_checked_at, created_at
		FROM tracked_files WHERE status = ?
		ORDER BY id`, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*TrackedFile
	for rows.Next() {
		f, err := d.scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (d *DB) IncrementRetryCount(id int64) error {
	_, err := d.db.Exec(`UPDATE tracked_files SET retry_count = retry_count + 1 WHERE id = ?`, id)
	return err
}

func (d *DB) ResetRetryCount(id int64) error {
	_, err := d.db.Exec(`UPDATE tracked_files SET retry_count = 0 WHERE id = ?`, id)
	return err
}

func (d *DB) ListPendingLimit(limit int) ([]*TrackedFile, error) {
	rows, err := d.db.Query(`
		SELECT id, absolute_path, file_size, mod_time, status,
			google_media_id, error_message, retry_count, uploaded_at, last_checked_at, created_at
		FROM tracked_files WHERE status = 'pending'
		ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*TrackedFile
	for rows.Next() {
		f, err := d.scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (d *DB) UpdateLastChecked(id int64) error {
	_, err := d.db.Exec(`UPDATE tracked_files SET last_checked_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (d *DB) PurgeUnseenFiles(before time.Time) (int, error) {
	result, err := d.db.Exec(`
		DELETE FROM tracked_files
		WHERE last_checked_at < ? AND status IN ('pending', 'success')
	`, before.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (d *DB) UpdateStatus(id int64, status Status, googleMediaID, errorMessage *string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	query := `UPDATE tracked_files SET status = ?, last_checked_at = ?`

	args := []interface{}{string(status), now}

	if googleMediaID != nil {
		query += `, google_media_id = ?`
		args = append(args, *googleMediaID)
	}
	if errorMessage != nil {
		query += `, error_message = ?`
		args = append(args, *errorMessage)
	}
	if status == StatusSuccess {
		query += `, uploaded_at = ?`
		args = append(args, now)
	}

	query += ` WHERE id = ?`
	args = append(args, id)

	_, err := d.db.Exec(query, args...)
	return err
}

func (d *DB) scanFile(row interface{}) (*TrackedFile, error) {
	var (
		tf                TrackedFile
		modTimeStr        string
		lastCheckedStr    string
		createdAtStr      string
		uploadedAtStr     sql.NullString
		googleMediaID     sql.NullString
		errorMessage      sql.NullString
	)

	scan := func(r interface{ Scan(dest ...interface{}) error }) error {
		return r.Scan(
			&tf.ID, &tf.AbsolutePath, &tf.FileSize, &modTimeStr,
			&tf.Status, &googleMediaID, &errorMessage, &tf.RetryCount,
			&uploadedAtStr, &lastCheckedStr, &createdAtStr,
		)
	}

	switch r := row.(type) {
	case *sql.Row:
		if err := scan(r); err != nil {
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("not found")
			}
			return nil, err
		}
	case *sql.Rows:
		if err := scan(r); err != nil {
			return nil, err
		}
	}

	tf.ModTime, _ = time.Parse(time.RFC3339, modTimeStr)
	tf.LastCheckedAt, _ = time.Parse(time.RFC3339, lastCheckedStr)
	tf.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)

	if uploadedAtStr.Valid {
		t := parseOrNil(uploadedAtStr.String)
		tf.UploadedAt = t
	}
	if googleMediaID.Valid {
		tf.GoogleMediaID = &googleMediaID.String
	}
	if errorMessage.Valid {
		tf.ErrorMessage = &errorMessage.String
	}

	return &tf, nil
}

func parseOrNil(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}
