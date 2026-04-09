package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type APIClient struct {
	BaseURL string
	Token   string // JWT session token
}

type Repo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UpdateTime int64  `json:"update_time"`
	Encrypted  bool   `json:"encrypted"`
}

type DirEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "file" or "dir"
	ID       string `json:"id"`
	Size     int64  `json:"size,omitempty"`
	Mtime    int64  `json:"mtime"`
	Modifier string `json:"modifier,omitempty"`
}

func NewClient(baseURL string) *APIClient {
	return &APIClient{BaseURL: baseURL}
}

// doRequest performs an authenticated HTTP request. If body is non-nil, it is
// JSON-encoded as the request body. If result is non-nil, the response is
// JSON-decoded into it.
func (c *APIClient) doRequest(method, path string, body, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to encode request: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, string(msg))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to parse response: %v", err)
		}
	}
	return nil
}

func (c *APIClient) Login(email, password string) error {
	var result struct {
		Token string `json:"token"`
	}
	err := c.doRequest("POST", "/api/v1/auth/login",
		map[string]string{"email": email, "password": password}, &result)
	if err != nil {
		return err
	}
	c.Token = result.Token
	return nil
}

func (c *APIClient) ListRepos() ([]Repo, error) {
	var repos []Repo
	err := c.doRequest("GET", "/api/v1/repos", nil, &repos)
	return repos, err
}

func (c *APIClient) CreateRepo(name string) (*Repo, error) {
	var repo Repo
	err := c.doRequest("POST", "/api/v1/repos", map[string]string{"name": name}, &repo)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (c *APIClient) ListDir(repoID, path string) ([]DirEntry, error) {
	if path == "" {
		path = "/"
	}
	escapedPath := url.QueryEscape(path)
	var entries []DirEntry
	err := c.doRequest("GET", fmt.Sprintf("/api/v1/repos/%s/dir/?path=%s", repoID, escapedPath), nil, &entries)
	return entries, err
}

func (c *APIClient) DeleteRepo(repoID string) error {
	return c.doRequest("DELETE", "/api/v1/repos/"+repoID, nil, nil)
}
