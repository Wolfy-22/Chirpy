package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func HashPassword(pswd string) (string, error) {
	hash, err := argon2id.CreateHash(pswd, argon2id.DefaultParams)
	if err != nil {
		return "", fmt.Errorf("error creating password hash: %w", err)
	}
	return hash, nil
}

func CheckPasswordHash(pswd, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(pswd, hash)
	if err != nil {
		return false, fmt.Errorf("error checking password: %w", err)
	}
	return match, nil

}

func MakeJWT(userId uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
	JWTtoken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "chirpy",
		IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(expiresIn)),
		Subject:   userId.String(),
	})

	signedToken, err := JWTtoken.SignedString([]byte(tokenSecret))
	if err != nil {
		return "", fmt.Errorf("error signing token: %w", err)
	}

	return signedToken, nil

}

func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) { return []byte(tokenSecret), nil })
	if err != nil {
		return uuid.Nil, fmt.Errorf("error parsing token: %w", err)
	}

	stringUserId, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, fmt.Errorf("error getting id: %w", err)
	}

	userId, err := uuid.Parse(stringUserId)
	if err != nil {
		return uuid.Nil, fmt.Errorf("error parsing user id: %w", err)
	}

	return userId, nil

}

func GetBearerToken(headers http.Header) (string, error) {

	token := headers.Get("Authorization")
	if len(token) == 0 {
		return "", fmt.Errorf("header does not exist")
	}
	strippedtoken := strings.Split(token, " ")
	return strippedtoken[1], nil
}
