package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/haiwen/seafile-server/fileserver/apitokenstore"
	"github.com/haiwen/seafile-server/fileserver/authmgr"
	"github.com/haiwen/seafile-server/fileserver/middleware"
	"github.com/haiwen/seafile-server/fileserver/repomgr"
	"github.com/haiwen/seafile-server/fileserver/share"
	log "github.com/sirupsen/logrus"
)

// SeaDriveAuthTokenHandler handles POST /api2/auth-token/
// This provides SeaDrive client compatibility for authentication.
func SeaDriveAuthTokenHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	email, err := authmgr.ValidatePassword(username, password)
	if err != nil {
		log.Infof("SeaDrive login failed for %s: %v", username, err)
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	// SeaDrive expects a 40-char hex API token (Seahub/DRF format), not a JWT.
	token, err := apitokenstore.Create(email)
	if err != nil {
		log.Errorf("Failed to generate API token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{Token: token})
}

// SeaDriveAuthPingHandler handles GET /api2/auth/ping/
// Returns "pong" to confirm token is valid. Matches Seahub behavior.
func SeaDriveAuthPingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`"pong"`))
}

// serverInfoResponse matches Seahub's /api2/server-info/ response shape.
type serverInfoResponse struct {
	Version  string   `json:"version"`
	Features []string `json:"features"`
}

// SeaDriveServerInfoHandler handles GET /api2/server-info/
func SeaDriveServerInfoHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, serverInfoResponse{
		Version:  "11.0.0",
		Features: []string{"seafile-basic"},
	})
}

// seadriveRepo is the Seahub-compatible shape for /api2/repos/ entries.
type seadriveRepo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Owner      string `json:"owner"`
	Permission string `json:"permission"`
	Type       string `json:"type"`
	Encrypted  bool   `json:"encrypted"`
	Size       int64  `json:"size"`
	MTime      int64  `json:"mtime"`
	HeadCommit string `json:"head_commit_id"`
	Version    int    `json:"version"`
	Root       string `json:"root"`
}

// SeaDriveReposHandler handles GET /api2/repos/
// Returns repos accessible to the authenticated user in Seahub's format.
func SeaDriveReposHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)

	seen := make(map[string]bool)
	var result []seadriveRepo

	add := func(repos []*share.SharedRepo, typ, ownerOverride string) {
		for _, repo := range repos {
			if repo.RepoType != "" {
				continue
			}
			if seen[repo.ID] {
				continue
			}
			seen[repo.ID] = true
			owner := repo.Owner
			if ownerOverride != "" {
				owner = ownerOverride
			}
			perm := repo.Permission
			if perm == "" {
				perm = "rw"
			}
			result = append(result, seadriveRepo{
				ID:         repo.ID,
				Name:       repo.Name,
				Owner:      owner,
				Permission: perm,
				Type:       typ,
				Encrypted:  false,
				Size:       0,
				MTime:      repo.MTime,
				HeadCommit: repo.HeadCommitID,
				Version:    repo.Version,
				Root:       "",
			})
		}
	}

	owned, err := share.GetReposByOwner(user)
	if err != nil {
		log.Errorf("Failed to get owned repos for %s: %v", user, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	add(owned, "repo", user)

	shared, err := share.ListShareRepos(user, "to_email")
	if err == nil {
		add(shared, "srepo", "")
	}

	group, err := share.GetGroupReposByUser(user, -1)
	if err == nil {
		add(group, "grepo", "")
	}

	if result == nil {
		result = []seadriveRepo{}
	}
	writeJSON(w, http.StatusOK, result)
}

// accountInfoResponse matches Seahub's /api2/account/info/ response shape.
type accountInfoResponse struct {
	Email       string `json:"email"`
	Name        string `json:"name"`
	Usage       int64  `json:"usage"`
	Total       int64  `json:"total"`
	Institution string `json:"institution"`
}

// SeaDriveAccountInfoHandler handles GET /api2/account/info/
// Returns the authenticated user's profile.
func SeaDriveAccountInfoHandler(w http.ResponseWriter, r *http.Request) {
	email := middleware.GetUserEmail(r)
	writeJSON(w, http.StatusOK, accountInfoResponse{
		Email:       email,
		Name:        email,
		Usage:       0,
		Total:       -1, // unlimited
		Institution: "",
	})
}

// downloadInfoResponse matches Seahub's /api2/repos/{id}/download-info/ response.
type downloadInfoResponse struct {
	RelayID       string `json:"relay_id"`
	RelayAddr     string `json:"relay_addr"`
	RelayPort     string `json:"relay_port"`
	Token         string `json:"token"`
	RepoID        string `json:"repo_id"`
	RepoName      string `json:"repo_name"`
	Email         string `json:"email"`
	RandomKey     string `json:"random_key"`
	EncVersion    int    `json:"enc_version"`
	Magic         string `json:"magic"`
	Salt          string `json:"salt"`
	Encrypted     bool   `json:"encrypted"`
	RepoVersion   int    `json:"repo_version"`
	HeadCommitID  string `json:"head_commit_id"`
	FileServerURL string `json:"file_server_url"`
}

// SeaDriveDownloadInfoHandler handles GET /api2/repos/{repoid}/download-info/
// Returns the sync token + repo metadata + file server URL so SeaDrive can
// start syncing the library.
func SeaDriveDownloadInfoHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)
	vars := mux.Vars(r)
	repoID := vars["repoid"]

	perm := share.CheckPerm(repoID, user)
	if perm == "" {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	repo := repomgr.Get(repoID)
	if repo == nil {
		http.Error(w, "Repo not found", http.StatusNotFound)
		return
	}

	token, err := repomgr.GenerateRepoToken(repoID, user)
	if err != nil {
		log.Errorf("Failed to generate repo token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Build file server URL from the request so it works regardless of
	// what host/port SeaDrive connected to.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	fileServerURL := scheme + "://" + r.Host

	writeJSON(w, http.StatusOK, downloadInfoResponse{
		RelayID:       "default_relay_id",
		RelayAddr:     "default_relay_addr",
		RelayPort:     "default_relay_port",
		Token:         token,
		RepoID:        repo.ID,
		RepoName:      repo.Name,
		Email:         user,
		RandomKey:     repo.RandomKey,
		EncVersion:    repo.EncVersion,
		Magic:         repo.Magic,
		Salt:          repo.Salt,
		Encrypted:     repo.IsEncrypted,
		RepoVersion:   repo.Version,
		HeadCommitID:  repo.HeadCommitID,
		FileServerURL: fileServerURL,
	})
}

// SeaDriveRepoTokenHandler handles POST /api2/repos/{repoid}/repo-tokens/
// This provides SeaDrive client compatibility for obtaining repo sync tokens.
func SeaDriveRepoTokenHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)
	vars := mux.Vars(r)
	repoID := vars["repoid"]

	perm := share.CheckPerm(repoID, user)
	if perm == "" {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	token, err := repomgr.GenerateRepoToken(repoID, user)
	if err != nil {
		log.Errorf("Failed to generate repo token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, syncTokenResponse{Token: token})
}
