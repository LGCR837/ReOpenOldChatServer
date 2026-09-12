package httpapi

import (
	"context"
	"errors"
	"strings"

	"metrochat/internal/data"
)

func (a *API) createGroupWithRetry(ctx context.Context, ownerID, name string) (string, error) {
	for i := 0; i < 3; i++ {
		groupID, err := newPublicID("GRP-", 6)
		if err != nil {
			return "", err
		}
		g := &data.Group{
			ID:               groupID,
			Name:             name,
			OwnerID:          ownerID,
			JoinApproval:     false,
			GlobalMute:       false,
			AnnouncementMode: 0,
		}
		if err := a.groups.Create(ctx, g, ownerID); err != nil {
			// SQLite 唯一约束冲突检查
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				continue
			}
			return "", err
		}
		return groupID, nil
	}
	return "", errors.New("group id collision")
}

func isValidGroupID(id string) bool {
	if len(id) < 4 || len(id) > 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return true
}

func isValidGroupName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	return true
}
