package keycache

import (
	"sync"
	"time"
)

const (
	KeyExpireTime   = 3600            // 1 hour
	ReaperInterval  = 60 * time.Second
)

type DecryptKey struct {
	Key        []byte
	IV         []byte
	Version    int
	ExpireTime int64
}

var keys sync.Map

func cacheKey(repoID, user string) string {
	return repoID + ":" + user
}

func SetKey(repoID, user string, key, iv []byte, version int) {
	dk := &DecryptKey{
		Key:        key,
		IV:         iv,
		Version:    version,
		ExpireTime: time.Now().Unix() + KeyExpireTime,
	}
	keys.Store(cacheKey(repoID, user), dk)
}

func GetKey(repoID, user string) *DecryptKey {
	k := cacheKey(repoID, user)
	val, ok := keys.Load(k)
	if !ok {
		return nil
	}
	dk := val.(*DecryptKey)

	if time.Now().Unix() >= dk.ExpireTime {
		keys.Delete(k)
		return nil
	}

	return dk
}

func DeleteKey(repoID, user string) {
	keys.Delete(cacheKey(repoID, user))
}

func StartReaper() {
	go func() {
		ticker := time.NewTicker(ReaperInterval)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now().Unix()
			keys.Range(func(key, value interface{}) bool {
				dk := value.(*DecryptKey)
				if now >= dk.ExpireTime {
					keys.Delete(key)
				}
				return true
			})
		}
	}()
}
