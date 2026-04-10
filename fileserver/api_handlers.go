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

	path, _ := url.QueryUnescape(r.URL.Query().Get("path"))
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

	path, _ := url.QueryUnescape(r.URL.Query().Get("path"))
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

	path, _ := url.QueryUnescape(r.URL.Query().Get("path"))
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

func renameHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)
	vars := mux.Vars(r)
	repoID := vars["repoid"]

	path, _ := url.QueryUnescape(r.URL.Query().Get("path"))
	newName, _ := url.QueryUnescape(r.URL.Query().Get("newname"))
	if path == "" || newName == "" {
		http.Error(w, "path and newname are required", http.StatusBadRequest)
		return
	}

	repo, head, ok := loadRepoAndCommit(w, repoID, user)
	if !ok {
		return
	}

	// Get the existing entry
	oldEntry, err := fsmgr.GetDirentByPath(repo.StoreID, head.RootID, path)
	if err != nil || oldEntry == nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	parentDir := filepath.Dir(path)
	oldName := filepath.Base(path)

	// Delete old entry
	rootAfterDel, err := DelFileFromTree(repo.StoreID, head.RootID, parentDir, oldName)
	if err != nil {
		log.Errorf("Failed to delete old entry: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Add new entry with same ID but new name
	newDent := fsmgr.NewDirent(oldEntry.ID, newName, oldEntry.Mode, time.Now().Unix(), oldEntry.Modifier, oldEntry.Size)
	var names []string
	newRootID, err := DoPostMultiFiles(repo, rootAfterDel, parentDir, []*fsmgr.SeafDirent{newDent}, user, false, &names)
	if err != nil {
		log.Errorf("Failed to add renamed entry: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	desc := fmt.Sprintf("Renamed \"%s\" to \"%s\"", oldName, newName)
	_, err = GenNewCommit(repo, head, newRootID, user, desc, false, "", false)
	if err != nil {
		log.Errorf("Failed to commit rename: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func moveHandler(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserEmail(r)
	vars := mux.Vars(r)
	repoID := vars["repoid"]

	srcPath, _ := url.QueryUnescape(r.URL.Query().Get("src"))
	dstPath, _ := url.QueryUnescape(r.URL.Query().Get("dst"))
	if srcPath == "" || dstPath == "" {
		http.Error(w, "src and dst are required", http.StatusBadRequest)
		return
	}

	repo, head, ok := loadRepoAndCommit(w, repoID, user)
	if !ok {
		return
	}

	// Get the existing entry
	srcEntry, err := fsmgr.GetDirentByPath(repo.StoreID, head.RootID, srcPath)
	if err != nil || srcEntry == nil {
		http.Error(w, "Source not found", http.StatusNotFound)
		return
	}

	srcDir := filepath.Dir(srcPath)
	srcName := filepath.Base(srcPath)
	dstDir := filepath.Dir(dstPath)
	dstName := filepath.Base(dstPath)

	// Phase 1: Add to destination
	newDent := fsmgr.NewDirent(srcEntry.ID, dstName, srcEntry.Mode, time.Now().Unix(), srcEntry.Modifier, srcEntry.Size)
	var names []string
	rootAfterAdd, err := DoPostMultiFiles(repo, head.RootID, dstDir, []*fsmgr.SeafDirent{newDent}, user, true, &names)
	if err != nil {
		log.Errorf("Failed to add to destination: %v", err)
		http.Error(w, "Failed to move: destination error", http.StatusInternalServerError)
		return
	}

	// Phase 2: Remove from source
	newRootID, err := DelFileFromTree(repo.StoreID, rootAfterAdd, srcDir, srcName)
	if err != nil {
		log.Errorf("Failed to remove from source: %v", err)
		http.Error(w, "Failed to move: source error", http.StatusInternalServerError)
		return
	}

	desc := fmt.Sprintf("Moved \"%s\"", srcName)
	_, err = GenNewCommit(repo, head, newRootID, user, desc, false, "", false)
	if err != nil {
		log.Errorf("Failed to commit move: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
