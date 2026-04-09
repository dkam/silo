package tokenstore

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	TokenExpireTime = 3600           // 1 hour
	CleanupInterval = 5 * time.Minute
)

type AccessInfo struct {
	RepoID     string
	ObjID      string
	Op         string
	User       string
	ExpireTime int64
	OneTime    bool
}

var tokens sync.Map

func CreateToken(repoID, objID, op, user string, oneTime bool) string {
	token := uuid.New().String()
	info := &AccessInfo{
		RepoID:     repoID,
		ObjID:      objID,
		Op:         op,
		User:       user,
		ExpireTime: time.Now().Unix() + TokenExpireTime,
		OneTime:    oneTime,
	}
	tokens.Store(token, info)
	return token
}

func QueryToken(token string) *AccessInfo {
	val, ok := tokens.LoadAndDelete(token)
	if !ok {
		return nil
	}
	info := val.(*AccessInfo)

	if time.Now().Unix() >= info.ExpireTime {
		return nil
	}

	if !info.OneTime {
		tokens.Store(token, info)
	}

	return info
}

func DeleteToken(token string) {
	tokens.Delete(token)
}

func StartCleanup() {
	go func() {
		ticker := time.NewTicker(CleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now().Unix()
			tokens.Range(func(key, value interface{}) bool {
				info := value.(*AccessInfo)
				if now >= info.ExpireTime {
					tokens.Delete(key)
				}
				return true
			})
		}
	}()
}
