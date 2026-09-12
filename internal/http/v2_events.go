package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"ReOpenOldChatServer/internal/data"
)

// emitAccountEvent 给单个账号的 pts 事件流追加一条事件，供 /v2/updates/difference 补差。
// 事件写入失败不能影响主业务（消息已经落库并回响应了），故只记日志。
func (a *API) emitAccountEvent(userID, eventType string, payload any, ptsCount int) {
	if a.updates == nil || userID == "" {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("pts: marshal %s payload: %v", eventType, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.updates.Append(ctx, userID, eventType, string(body), ptsCount); err != nil {
		log.Printf("pts: append %s for %s: %v", eventType, userID, err)
	}
}

// emitAccountEventMany 群事件的扇出入口：给每个成员各存一份，
// 各自 pts 独立递增，成员之间不会互相干扰补差进度。
func (a *API) emitAccountEventMany(userIDs []string, eventType string, payload any, ptsCount int) {
	if a.updates == nil || len(userIDs) == 0 {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("pts: marshal %s payload: %v", eventType, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, id := range userIDs {
		if id == "" {
			continue
		}
		if err := a.updates.Append(ctx, id, eventType, string(body), ptsCount); err != nil {
			log.Printf("pts: append %s for %s: %v", eventType, id, err)
		}
	}
}

// groupMemberIDs 取群成员账号 ID 列表，发群事件时用。
func (a *API) groupMemberIDs(ctx context.Context, groupID string) []string {
	ids, err := a.groups.ListMemberIDs(ctx, groupID)
	if err != nil {
		return nil
	}
	return ids
}

// emitGroupMembershipChange 群成员变动时通知全体成员刷新成员列表。
// changedUserID 非空则同时给该用户发一条，使其即使刚进群也能收到自己的 pts 基线。
func (a *API) emitGroupMembershipChange(ctx context.Context, groupID, changedUserID string) {
	memberIDs := a.groupMemberIDs(ctx, groupID)
	if changedUserID != "" {
		found := false
		for _, id := range memberIDs {
			if id == changedUserID {
				found = true
				break
			}
		}
		if !found {
			memberIDs = append(memberIDs, changedUserID)
		}
	}
	a.emitAccountEventMany(memberIDs, "GROUP_MEMBERSHIP_CHANGE", map[string]any{
		"group_id": groupID,
	}, 1)
}

var _ = data.MaxPTSGap
