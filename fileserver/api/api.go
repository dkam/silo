package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/haiwen/seafile-server/fileserver/authmgr"
	"github.com/haiwen/seafile-server/fileserver/middleware"
	"github.com/haiwen/seafile-server/fileserver/repomgr"
	"github.com/haiwen/seafile-server/fileserver/share"
	"github.com/haiwen/seafile-server/fileserver/tokenstore"
	log "github.com/sirupsen/logrus"
)

var seafileDB *sql.DB

func Init(sDB *sql.DB) {
	seafileDB = sDB
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		http.Error(w, "Email and password are required", http.StatusBadRequest)
		return
	}

	email, err := authmgr.ValidatePassword(req.Email, req.Password)
	if err != nil {
		log.Infof("Login failed for %s: %v", req.Email, err)
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	token, err := authmgr.GenerateSessionToken(email)
	if err != nil {
		log.Errorf("Failed to generate session token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loginResponse{Token: token})
}

type accessTokenRequest struct {
	RepoID  string `json:"repo_id"`
	ObjID   string `json:"obj_id"`
	Op      string `json:"op"`
	OneTime bool   `json:"one_time"`
}

type accessTokenResponse struct {
	Token string `json:"token"`
}

func CreateAccessTokenHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)

	var req accessTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.RepoID == "" || req.Op == "" {
		http.Error(w, "repo_id and op are required", http.StatusBadRequest)
		return
	}

	token := tokenstore.CreateToken(req.RepoID, req.ObjID, req.Op, user, req.OneTime)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(accessTokenResponse{Token: token})
}

type repoInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UpdateTime int64  `json:"update_time"`
	Encrypted  bool   `json:"encrypted"`
}

func ListReposHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)

	// Get repo IDs owned by this user
	rows, err := seafileDB.Query(
		"SELECT o.repo_id, i.name, i.update_time, i.is_encrypted "+
			"FROM RepoOwner o LEFT JOIN RepoInfo i ON o.repo_id = i.repo_id "+
			"WHERE o.owner_id = ?", user)
	if err != nil {
		log.Errorf("Failed to query repos: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	repos := make([]repoInfo, 0)
	for rows.Next() {
		var repo repoInfo
		var name, isEncrypted sql.NullString
		var updateTime sql.NullInt64
		if err := rows.Scan(&repo.ID, &name, &updateTime, &isEncrypted); err != nil {
			continue
		}
		repo.Name = name.String
		repo.UpdateTime = updateTime.Int64
		repo.Encrypted = isEncrypted.String == "1"
		repos = append(repos, repo)
	}

	// Also get repos shared to this user
	sharedRows, err := seafileDB.Query(
		"SELECT s.repo_id, i.name, i.update_time, i.is_encrypted "+
			"FROM SharedRepo s LEFT JOIN RepoInfo i ON s.repo_id = i.repo_id "+
			"WHERE s.to_email = ?", user)
	if err == nil {
		defer sharedRows.Close()
		for sharedRows.Next() {
			var repo repoInfo
			var name, isEncrypted sql.NullString
			var updateTime sql.NullInt64
			if err := sharedRows.Scan(&repo.ID, &name, &updateTime, &isEncrypted); err != nil {
				continue
			}
			repo.Name = name.String
			repo.UpdateTime = updateTime.Int64
			repo.Encrypted = isEncrypted.String == "1"
			repos = append(repos, repo)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repos)
}

type syncTokenResponse struct {
	Token string `json:"token"`
}

func CreateRepoSyncTokenHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)
	vars := mux.Vars(r)
	repoID := vars["repoid"]

	// Verify repo exists
	repo := repomgr.Get(repoID)
	if repo == nil {
		http.Error(w, "Repo not found", http.StatusNotFound)
		return
	}

	// Verify user has access
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(syncTokenResponse{Token: token})
}
