package utils

import (
	"fmt"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/dkam/silo/fileserver/option"
)

func IsValidUUID(u string) bool {
	_, err := uuid.Parse(u)
	return err == nil
}

func IsObjectIDValid(objID string) bool {
	if len(objID) != 40 {
		return false
	}
	for i := 0; i < len(objID); i++ {
		c := objID[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

type SeahubClaims struct {
	IsInternal bool `json:"is_internal"`
	jwt.RegisteredClaims
}

func GenSeahubJWTToken() (string, error) {
	claims := new(SeahubClaims)
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Second * 300))
	claims.IsInternal = true

	token := jwt.NewWithClaims(jwt.GetSigningMethod("HS256"), claims)
	tokenString, err := token.SignedString([]byte(option.JWTPrivateKey))
	if err != nil {
		err := fmt.Errorf("failed to gen seahub jwt token: %w", err)
		return "", err
	}

	return tokenString, nil
}

type MyClaims struct {
	RepoID   string `json:"repo_id"`
	UserName string `json:"username"`
	jwt.RegisteredClaims
}

func GenNotifJWTToken(repoID, user string, exp int64) (string, error) {
	claims := new(MyClaims)
	claims.ExpiresAt = jwt.NewNumericDate(time.Unix(exp, 0))
	claims.RepoID = repoID
	claims.UserName = user

	token := jwt.NewWithClaims(jwt.GetSigningMethod("HS256"), claims)
	tokenString, err := token.SignedString([]byte(option.JWTPrivateKey))
	if err != nil {
		err := fmt.Errorf("failed to gen jwt token for repo %s: %w", repoID, err)
		return "", err
	}

	return tokenString, nil
}
