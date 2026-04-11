// Package notif implements the in-process Seafile notification server.
//
// It exposes a WebSocket endpoint that SeaDrive and Seafile Desktop can
// connect to so they receive push events when a repo's head commit changes,
// avoiding the default ~30s polling interval.
//
// The package is a simplified port of the upstream
// haiwen/seafile-server notification-server, collapsed to run inside the
// silo fileserver process. It currently only handles "repo-update" events
// because silo does not yet ship file locking, sharing API, or comments —
// the other upstream event types have no producer.
package notif

import (
	"sync"
	"sync/atomic"
)

var (
	// subMu protects subscriptions and (transitively) each subscribers.clients
	// map. Held in write mode for subscribe/unsubscribe, read mode for
	// NotifyRepoUpdate's snapshot.
	subMu         sync.RWMutex
	subscriptions map[string]*subscribers

	nextClientID uint64
)

type subscribers struct {
	clients map[uint64]*Client
}

// Init resets package state. Call once at server startup.
func Init() {
	subMu.Lock()
	subscriptions = make(map[string]*subscribers)
	subMu.Unlock()
}

func nextID() uint64 {
	return atomic.AddUint64(&nextClientID, 1)
}

func addSubscription(repoID string, c *Client) {
	subMu.Lock()
	defer subMu.Unlock()
	subs, ok := subscriptions[repoID]
	if !ok {
		subs = &subscribers{clients: make(map[uint64]*Client)}
		subscriptions[repoID] = subs
	}
	subs.clients[c.ID] = c
}

// removeSubscription removes a client's subscription for repoID. When the
// last subscriber leaves a repo, the outer map entry is deleted so
// subscriptions doesn't grow unbounded across the lifetime of the process.
func removeSubscription(repoID string, c *Client) {
	subMu.Lock()
	defer subMu.Unlock()
	subs, ok := subscriptions[repoID]
	if !ok {
		return
	}
	delete(subs.clients, c.ID)
	if len(subs.clients) == 0 {
		delete(subscriptions, repoID)
	}
}

// snapshotSubscribers returns a copy of the clients currently subscribed to
// repoID, or nil if no one is subscribed.
func snapshotSubscribers(repoID string) []*Client {
	subMu.RLock()
	defer subMu.RUnlock()
	subs, ok := subscriptions[repoID]
	if !ok || len(subs.clients) == 0 {
		return nil
	}
	out := make([]*Client, 0, len(subs.clients))
	for _, c := range subs.clients {
		out = append(out, c)
	}
	return out
}
