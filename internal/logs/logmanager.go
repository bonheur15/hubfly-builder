package logs

import (
	"fmt"
	"log"
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

func (m *LogManager) GetLog(logPath string) ([]byte, error) {
	return os.ReadFile(logPath)
}

func (m *LogManager) Cleanup(maxAge time.Duration) error {
	log.Printf("Running log cleanup, max age: %s", maxAge)
	files, err := os.ReadDir(m.logDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		info, err := file.Info()
		if err != nil {
			log.Printf("WARN: could not get info for log file %s: %v", file.Name(), err)
			continue
		}
		if time.Since(info.ModTime()) > maxAge {
			logPath := filepath.Join(m.logDir, file.Name())
			log.Printf("Deleting old log file: %s", logPath)
			if err := os.Remove(logPath); err != nil {
				log.Printf("WARN: could not delete old log file %s: %v", logPath, err)
			}
		}
	}
	return nil
}
