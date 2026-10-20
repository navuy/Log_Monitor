package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type loginPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type sessionClaims struct {
	Username string `json:"username"`
	Expires  int64  `json:"expires"`
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload loginPayload
	if json.NewDecoder(r.Body).Decode(&payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(payload.Username), []byte(a.adminUser)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(payload.Password), []byte(a.adminPass)) == 1
	if !userOK || !passOK {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": a.signSession(sessionClaims{Username: payload.Username, Expires: time.Now().Add(12 * time.Hour).Unix()}),
		"user":  payload.Username,
	})
}

func (a *App) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || !a.validSession(token) {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) signSession(claims sessionClaims) string {
	payload, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) validSession(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var claims sessionClaims
	return json.Unmarshal(payload, &claims) == nil && claims.Username == a.adminUser && claims.Expires > time.Now().Unix()
}
