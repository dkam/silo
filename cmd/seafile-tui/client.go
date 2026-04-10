package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

func (c *APIClient) Mkdir(repoID, path string) error {
	return c.doRequest("POST", fmt.Sprintf("/api/v1/repos/%s/mkdir?path=%s", repoID, url.QueryEscape(path)), nil, nil)
}

func (c *APIClient) DeleteFile(repoID, path string) error {
	return c.doRequest("DELETE", fmt.Sprintf("/api/v1/repos/%s/file?path=%s", repoID, url.QueryEscape(path)), nil, nil)
}

func (c *APIClient) DownloadFile(repoID, repoPath, localPath string) error {
	escapedPath := url.QueryEscape(repoPath)
	downloadURL := fmt.Sprintf("%s/api/v1/repos/%s/download?path=%s", c.BaseURL, repoID, escapedPath)

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	// Don't follow redirects — we need to follow with auth-less request to /files/
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound {
		// Follow the redirect to /files/{token}/filename
		loc := resp.Header.Get("Location")
		if loc == "" {
			return fmt.Errorf("redirect with no location")
		}
		// Build absolute URL if relative
		if loc[0] == '/' {
			loc = c.BaseURL + loc
		}
		fileResp, err := http.Get(loc)
		if err != nil {
			return fmt.Errorf("download failed: %v", err)
		}
		defer fileResp.Body.Close()

		if fileResp.StatusCode >= 400 {
			msg, _ := io.ReadAll(fileResp.Body)
			return fmt.Errorf("download failed: %s", string(msg))
		}

		out, err := os.Create(localPath)
		if err != nil {
			return fmt.Errorf("failed to create local file: %v", err)
		}
		defer out.Close()

		if _, err := io.Copy(out, fileResp.Body); err != nil {
			return fmt.Errorf("failed to write file: %v", err)
		}
		return nil
	}

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed: %s: %s", resp.Status, string(msg))
	}

	return fmt.Errorf("unexpected response: %d", resp.StatusCode)
}

// UploadFile uploads a local file to a repo directory.
// It creates an access token, then POSTs a multipart form to /upload-api/.
func (c *APIClient) UploadFile(repoID, parentDir, localPath string) error {
	// Step 1: Create access token with op=upload
	objID, _ := json.Marshal(map[string]string{"parent_dir": parentDir})
	var tokenResp struct {
		Token string `json:"token"`
	}
	err := c.doRequest("POST", "/api/v1/access-tokens", map[string]interface{}{
		"repo_id": repoID,
		"obj_id":  string(objID),
		"op":      "upload",
	}, &tokenResp)
	if err != nil {
		return fmt.Errorf("failed to get upload token: %v", err)
	}

	// Step 2: Build multipart form
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("parent_dir", parentDir); err != nil {
		return fmt.Errorf("failed to write parent_dir field: %v", err)
	}
	if err := writer.WriteField("ret-json", "1"); err != nil {
		return fmt.Errorf("failed to write ret-json field: %v", err)
	}

	part, err := writer.CreateFormFile("file", filepath.Base(localPath))
	if err != nil {
		return fmt.Errorf("failed to create form file: %v", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("failed to copy file content: %v", err)
	}
	writer.Close()

	// Step 3: POST to /upload-api/{token}
	uploadURL := fmt.Sprintf("%s/upload-api/%s", c.BaseURL, tokenResp.Token)
	req, err := http.NewRequest("POST", uploadURL, &buf)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed: %s: %s", resp.Status, string(msg))
	}

	return nil
}
