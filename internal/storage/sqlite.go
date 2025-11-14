package storage

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Storage struct {
	db *sql.DB
}

func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if err := createTables(db); err != nil {
		return nil, err
	}

	return &Storage{db: db}, nil
}

func createTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS build_jobs (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			user_id TEXT,
			source_type TEXT,
			source_info TEXT,
			build_config TEXT,
			status TEXT,
			image_tag TEXT,
			started_at DATETIME NULL,
			finished_at DATETIME NULL,
			exit_code INT NULL,
			retry_count INT DEFAULT 0,
			log_path TEXT,
			last_checkpoint TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME
		)
	`)
	return err
}

type BuildJob struct {
	ID             string
	ProjectID      string
	UserID         string
	SourceType     string
	SourceInfo     string // JSON
	BuildConfig    string // JSON
	Status         string
	ImageTag       string
	StartedAt      sql.NullTime
	FinishedAt     sql.NullTime
	ExitCode       sql.NullInt64
	RetryCount     int
	LogPath        string
	LastCheckpoint string // JSON
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
