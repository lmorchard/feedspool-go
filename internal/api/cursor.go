package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/database"
)

// cursorVersion is carried in every encoded cursor so a future format change
// is detectable rather than silently misparsed into a wrong position.
const cursorVersion = 1

type itemCursorPayload struct {
	Version       int     `json:"v"`
	DateRank      int     `json:"r"`
	EffectiveDate float64 `json:"d"`
	ID            int64   `json:"i"`
}

// encodeItemCursor renders a cursor as an opaque string. The encoding is
// deliberately not part of the v1 contract -- clients must treat it as a token
// to hand back, not a structure to read.
func encodeItemCursor(cursor *database.ItemCursor) string {
	if cursor == nil {
		return ""
	}
	payload, err := json.Marshal(itemCursorPayload{
		Version:       cursorVersion,
		DateRank:      cursor.DateRank,
		EffectiveDate: cursor.EffectiveDate,
		ID:            cursor.ID,
	})
	if err != nil {
		// The payload is four scalars; Marshal cannot fail on it.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// decodeItemCursor parses a cursor produced by encodeItemCursor.
//
// Any failure is an error rather than a zero-valued cursor: silently starting
// from the beginning would make a client's paging loop repeat forever without
// ever reporting a problem.
func decodeItemCursor(encoded string) (*database.ItemCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("cursor is not valid base64url: %w", err)
	}

	var payload itemCursorPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("cursor payload is malformed: %w", err)
	}
	if payload.Version != cursorVersion {
		return nil, fmt.Errorf("cursor version %d is not supported", payload.Version)
	}
	if payload.DateRank != 0 && payload.DateRank != 1 {
		return nil, fmt.Errorf("cursor date rank %d is out of range", payload.DateRank)
	}

	return &database.ItemCursor{
		DateRank:      payload.DateRank,
		EffectiveDate: payload.EffectiveDate,
		ID:            payload.ID,
	}, nil
}

// encodeFeedCursor and decodeFeedCursor wrap the feed cursor, which is just the
// last URL returned. It is encoded anyway so that both collection endpoints
// present cursors as the same kind of opaque token.
func encodeFeedCursor(after string) string {
	if after == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(after))
}

func decodeFeedCursor(encoded string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("cursor is not valid base64url: %w", err)
	}
	return string(raw), nil
}
