package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const sessionCookieName = "dota_hub_session"

type contextKey string

const userContextKey contextKey = "userID"

func userIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(userContextKey).(string); ok {
		return value
	}
	return ""
}

func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userContextKey, userID)
}

func (server *Server) ensureAnonymousSession(response http.ResponseWriter, request *http.Request) (*http.Request, error) {
	token := ""
	if cookie, err := request.Cookie(sessionCookieName); err == nil {
		token = strings.TrimSpace(cookie.Value)
	}

	created := false
	if token == "" {
		var err error
		token, err = randomToken(32)
		if err != nil {
			return request, err
		}
		created = true
	}

	userID, found, err := server.store.EnsureAnonymousUser(request.Context(), hashSessionToken(token))
	if err != nil {
		return request, err
	}
	if !found && !created {
		token, err = randomToken(32)
		if err != nil {
			return request, err
		}
		userID, _, err = server.store.EnsureAnonymousUser(request.Context(), hashSessionToken(token))
		if err != nil {
			return request, err
		}
		created = true
	}
	if created {
		http.SetCookie(response, &http.Cookie{
			Name:     sessionCookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   int((180 * 24 * time.Hour).Seconds()),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   request.TLS != nil,
		})
	}
	return request.WithContext(withUserID(request.Context(), userID)), nil
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("не удалось создать пользовательскую сессию: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
