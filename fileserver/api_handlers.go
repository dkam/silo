package main

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/haiwen/seafile-server/fileserver/commitmgr"
	"github.com/haiwen/seafile-server/fileserver/fsmgr"
	"github.com/haiwen/seafile-server/fileserver/middleware"
	"github.com/haiwen/seafile-server/fileserver/repomgr"
	"github.com/haiwen/seafile-server/fileserver/share"
	"github.com/haiwen/seafile-server/fileserver/tokenstore"
	log "github.com/sirupsen/logrus"
)

// loadRepoAndCommit loads the repo and its head commit, with rw permission check.
func loadRepoAndCommit(w http.ResponseWriter, repoID, user string) (*repomgr.Repo, *commitmgr.Commit, bool) {
	perm := share.CheckPerm(repoID, user)
	if perm != "rw" {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return nil, nil, false
	}
	repo := repomgr.Get(repoID)
	if repo == nil {
		http.Error(w, "Repo not found", http.StatusNotFound)
		return nil, nil, false
	}
	head, err := commitmgr.Load(repo.ID, repo.HeadCommitID)
	if err != nil {
		log.Errorf("Failed to load head commit: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil, nil, false
	}
	return repo, head, true
}

func mkdirHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)
	vars := mux.Vars(r)
	repoID := vars["repoid"]

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	repo, head, ok := loadRepoAndCommit(w, repoID, user)
	if !ok {
		return
	}

	parentDir := filepath.Dir(path)
	dirName := filepath.Base(path)

	mode := uint32(syscall.S_IFDIR | 0644)
	dent := fsmgr.NewDirent(fsmgr.EmptySha1, dirName, mode, time.Now().Unix(), "", 0)

	var names []string
	newRootID, err := DoPostMultiFiles(repo, head.RootID, parentDir, []*fsmgr.SeafDirent{dent}, user, false, &names)
	if err != nil {
		log.Errorf("Failed to create directory: %v", err)
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	desc := fmt.Sprintf("Added directory \"%s\"", dirName)
	_, err = GenNewCommit(repo, head, newRootID, user, desc, false, "", false)
	if err != nil {
		log.Errorf("Failed to commit mkdir: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)
	vars := mux.Vars(r)
	repoID := vars["repoid"]

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	repo, head, ok := loadRepoAndCommit(w, repoID, user)
	if !ok {
		return
	}

	parentDir := filepath.Dir(path)
	filename := filepath.Base(path)

	newRootID, err := DelFileFromTree(repo.StoreID, head.RootID, parentDir, filename)
	if err != nil {
		log.Errorf("Failed to delete %s: %v", path, err)
		http.Error(w, fmt.Sprintf("Failed to delete: %v", err), http.StatusNotFound)
		return
	}

	desc := fmt.Sprintf("Deleted \"%s\"", filename)
	_, err = GenNewCommit(repo, head, newRootID, user, desc, false, "", false)
	if err != nil {
		log.Errorf("Failed to commit delete: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func downloadFileHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)
	vars := mux.Vars(r)
	repoID := vars["repoid"]

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

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

	fileID, mode, err := fsmgr.GetObjIDByPath(repo.StoreID, repo.RootID, path)
	if err != nil || fileID == "" {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if fsmgr.IsDir(mode) {
		http.Error(w, "Cannot download a directory", http.StatusBadRequest)
		return
	}

	filename := filepath.Base(path)
	token := tokenstore.CreateToken(repoID, fileID, "download", user, true)
	redirectURL := fmt.Sprintf("/files/%s/%s", token, url.PathEscape(filename))
	http.Redirect(w, r, redirectURL, http.StatusFound)
}
