package api

import (
	"bytes"
	"encoding/json"
	"net/http"

	"hubfly-builder/internal/storage"
)

type Client struct {
	backendURL string
	httpClient *http.Client
}

func NewClient(backendURL string) *Client {
	return &Client{
		backendURL: backendURL,
		httpClient: &http.Client{},
	}
}

func (c *Client) ReportStatus(job *storage.BuildJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.backendURL, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
