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

func (s *Storage) CreateJob(job *BuildJob) error {
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()
	job.Status = "pending"

	_, err := s.db.Exec(`
		INSERT INTO build_jobs (id, project_id, user_id, source_type, source_info, build_config, status, image_tag, started_at, finished_at, exit_code, retry_count, log_path, last_checkpoint, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, job.ID, job.ProjectID, job.UserID, job.SourceType, job.SourceInfo, job.BuildConfig, job.Status, job.ImageTag, job.StartedAt, job.FinishedAt, job.ExitCode, job.RetryCount, job.LogPath, job.LastCheckpoint, job.CreatedAt, job.UpdatedAt)

	return err
}

func (s *Storage) GetJob(id string) (*BuildJob, error) {
	job := &BuildJob{}
	err := s.db.QueryRow(`
		SELECT id, project_id, user_id, source_type, source_info, build_config, status, image_tag, started_at, finished_at, exit_code, retry_count, log_path, last_checkpoint, created_at, updated_at
		FROM build_jobs WHERE id = ?
	`, id).Scan(&job.ID, &job.ProjectID, &job.UserID, &job.SourceType, &job.SourceInfo, &job.BuildConfig, &job.Status, &job.ImageTag, &job.StartedAt, &job.FinishedAt, &job.ExitCode, &job.RetryCount, &job.LogPath, &job.LastCheckpoint, &job.CreatedAt, &job.UpdatedAt)

	if err != nil {
		return nil, err
	}

	return job, nil
}
