package notif

import (
	"encoding/json"
	"runtime/debug"

	log "github.com/sirupsen/logrus"
)

// Event type strings exchanged over the wire.
const (
	EventTypeRepoUpdate = "repo-update"
	EventTypeJWTExpired = "jwt-expired"
)

// Message is the wire format exchanged with clients. Both inbound
// (subscribe/unsubscribe) and outbound (repo-update, jwt-expired) frames use
// it.
type Message struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

// RepoUpdateEvent is the payload for a repo-update message.
type RepoUpdateEvent struct {
	RepoID   string `json:"repo_id"`
	CommitID string `json:"commit_id"`
}

// NotifyRepoUpdate fans a repo-update event out to every client currently
// subscribed to repoID. Delivery is best-effort and non-blocking: if a
// client's write channel is full, the message is dropped for that client
// rather than back-pressuring the caller (which is the commit-write hot path).
func NotifyRepoUpdate(repoID, commitID string) {
	targets := snapshotSubscribers(repoID)
	if len(targets) == 0 {
		return
	}

	content, err := json.Marshal(&RepoUpdateEvent{RepoID: repoID, CommitID: commitID})
	if err != nil {
		log.Warnf("notif: failed to encode repo-update event: %v", err)
		return
	}
	msg := &Message{Type: EventTypeRepoUpdate, Content: content}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("notif: panic in fanout: %v\n%s", r, debug.Stack())
			}
		}()
		for _, c := range targets {
			select {
			case c.wch <- msg:
			default:
				// Slow consumer: drop rather than block the commit path.
				log.Debugf("notif: dropping repo-update for slow client %d", c.ID)
			}
		}
	}()
}
