package notif

import (
	"encoding/json"
	"testing"
	"time"
)

// fakeClient builds a minimal Client suitable for driving the fanout path
// without a real WebSocket connection.
func fakeClient() *Client {
	return &Client{
		ID:    nextID(),
		wch:   make(chan *Message, wchBuffer),
		repos: make(map[string]int64),
	}
}

func TestNotifyRepoUpdateFanout(t *testing.T) {
	Init()

	const repoID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	const commitID = "0123456789abcdef0123456789abcdef01234567"

	c1 := fakeClient()
	c2 := fakeClient()
	other := fakeClient()

	addSubscription(repoID, c1)
	addSubscription(repoID, c2)
	addSubscription("11111111-2222-3333-4444-555555555555", other)

	NotifyRepoUpdate(repoID, commitID)

	for i, c := range []*Client{c1, c2} {
		select {
		case msg := <-c.wch:
			if msg.Type != "repo-update" {
				t.Errorf("client %d: got type %q, want repo-update", i, msg.Type)
			}
			var ev RepoUpdateEvent
			if err := json.Unmarshal(msg.Content, &ev); err != nil {
				t.Fatalf("client %d: bad content: %v", i, err)
			}
			if ev.RepoID != repoID || ev.CommitID != commitID {
				t.Errorf("client %d: got %+v, want repo=%s commit=%s", i, ev, repoID, commitID)
			}
		case <-time.After(time.Second):
			t.Errorf("client %d: timed out waiting for repo-update", i)
		}
	}

	// The unrelated subscriber must not have received anything.
	select {
	case msg := <-other.wch:
		t.Errorf("unrelated client got unexpected message: %+v", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNotifyRepoUpdateNoSubscribers(t *testing.T) {
	Init()
	// Should not panic and should return quickly when nothing is subscribed.
	NotifyRepoUpdate("nobody-home", "deadbeef")
}

func TestRemoveSubscriptionStopsDelivery(t *testing.T) {
	Init()

	const repoID = "ffffffff-eeee-dddd-cccc-bbbbbbbbbbbb"
	c := fakeClient()
	addSubscription(repoID, c)
	removeSubscription(repoID, c)

	NotifyRepoUpdate(repoID, "0000000000000000000000000000000000000000")

	select {
	case msg := <-c.wch:
		t.Errorf("unsubscribed client got message: %+v", msg)
	case <-time.After(50 * time.Millisecond):
	}
}
