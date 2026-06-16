package wbstream

import (
	"context"
	"fmt"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
)

// Provider produces LiveKit credentials for the WB Stream service.
type Provider struct{}

// Engine reports which engine consumes credentials from this auth provider.
func (Provider) Engine() string { return "livekit" }

// DefaultServiceURL returns the WB Stream service URL.
func (Provider) DefaultServiceURL() string { return "https://stream.wb.ru" }

// Issue runs the WB Stream auth flow and returns LiveKit credentials.
//
// When cfg.AccountToken is set the caller authenticates as the room owner: the
// account token is used directly to fetch the room connection token. This is
// how an olcrtc server opens and holds a room so guest clients can join (WB
// Stream rooms only accept guests once an owner is connected). Otherwise a
// guest is registered and joined - the path olcrtc clients take.
func (Provider) Issue(ctx context.Context, cfg auth.Config) (auth.Credentials, error) {
	if cfg.RoomURL == "" || cfg.RoomURL == "any" {
		return auth.Credentials{}, auth.ErrRoomIDRequired
	}

	roomID := cfg.RoomURL
	accessToken := cfg.AccountToken
	if accessToken == "" {
		guest, err := registerGuest(ctx, cfg.Name)
		if err != nil {
			return auth.Credentials{}, fmt.Errorf("register guest: %w", err)
		}
		if err := joinRoom(ctx, guest, roomID); err != nil {
			return auth.Credentials{}, fmt.Errorf("join room: %w", err)
		}
		accessToken = guest
	}

	tok, err := getToken(ctx, accessToken, roomID, cfg.Name)
	if err != nil {
		return auth.Credentials{}, fmt.Errorf("get token: %w", err)
	}

	url := tok.ServerURL
	if url == "" {
		url = defaultWSURL
	}

	return auth.Credentials{
		URL:   url,
		Token: tok.RoomToken,
		Extra: map[string]string{"roomID": roomID},
	}, nil
}

// CreateRoom creates a new WB Stream room on the account identified by
// cfg.AccountToken and returns the room ID. Implements auth.RoomCreator so the
// session layer can mint a room for an owner-mode server at startup.
func (Provider) CreateRoom(ctx context.Context, cfg auth.Config) (string, error) {
	if cfg.AccountToken == "" {
		return "", ErrTokenRequired
	}
	roomID, err := createRoom(ctx, cfg.AccountToken)
	if err != nil {
		return "", fmt.Errorf("create room: %w", err)
	}
	return roomID, nil
}

func init() { //nolint:gochecknoinits // auth registration is the canonical Go pattern for plugins
	auth.Register("wbstream", Provider{})
}
