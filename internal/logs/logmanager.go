package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type LogManager struct {
	logDir string
}

func NewLogManager(logDir string) (*LogManager, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	return &LogManager{logDir: logDir}, nil
}

func (m *LogManager) CreateLogFile(jobID string) (string, *os.File, error) {
	ts := time.Now().UTC().Format("20060102T150405Z")
	logName := fmt.Sprintf("build-%s-%s.log", jobID, ts)
	logPath := filepath.Join(m.logDir, logName)

	f, err := os.Create(logPath)
	if err != nil {
		return "", nil, err
	}

	return logPath, f, nil
}
