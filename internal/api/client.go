package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"hubfly-builder/internal/storage"
)

type Client struct {
	httpClient  *http.Client
	callbackURL string
}

func NewClient(callbackURL string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		callbackURL: callbackURL,
	}
}

type ReportPayload struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"projectId"`
	UserID          string    `json:"userId"`
	Status          string    `json:"status"`
	ImageTag        string    `json:"imageTag,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt"`
	DurationSeconds float64   `json:"durationSeconds"`
	LogPath         string    `json:"logPath"`
	Error           string    `json:"error,omitempty"`
}

func (c *Client) ReportResult(job *storage.BuildJob, status, errorMsg string) error {
	if c.callbackURL == "" {
		return nil // No callback URL configured
	}

	payload := ReportPayload{
		ID:              job.ID,
		ProjectID:       job.ProjectID,
		UserID:          job.UserID,
		Status:          status,
		ImageTag:        job.ImageTag,
		LogPath:         job.LogPath,
		Error:           errorMsg,
	}
	if !job.StartedAt.Time.IsZero() {
		payload.StartedAt = job.StartedAt.Time
		payload.FinishedAt = time.Now()
		payload.DurationSeconds = payload.FinishedAt.Sub(payload.StartedAt).Seconds()
	}


	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.callbackURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// TODO: Handle non-2xx responses and implement retries
	return nil
}