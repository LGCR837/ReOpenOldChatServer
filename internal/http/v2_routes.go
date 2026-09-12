package httpapi

import (
	"github.com/go-chi/chi/v5"
)

// v1 全量保留兼容旧客户端；v2 复用同一批 handler，差异只在中间件层。
func (api *API) registerV2Routes(r chi.Router) {
	r.Use(api.secureMiddleware)
	r.Use(api.v2SignMiddleware)

	r.Post("/gateway", api.handleV2Gateway)

	r.Group(func(r chi.Router) {
		r.Use(api.authMiddleware)

		// ---- 私聊 ----
		r.Post("/direct/send", api.handleDirectSend)
		r.Post("/direct/read", api.handleDirectRead)
		r.Get("/direct/messages/v2", api.handleDirectMessagesV2)
		r.Get("/direct/messages/search", api.handleDirectMessagesSearch)
		r.Get("/direct/messages", api.handleDirectMessages)
		r.Get("/direct/messages/after", api.handleDirectMessagesAfter)
		r.Delete("/direct/messages/{messageID}", api.handleDirectMessageDelete)
		r.Post("/unread/direct", api.handleDirectUnread) // v1: /direct/unread

		// ---- 群 ----
		r.Post("/groups/create", api.handleGroupCreate)
		r.Post("/groups/join", api.handleGroupJoin)
		r.Post("/groups/leave", api.handleGroupLeave)
		r.Post("/groups/invite", api.handleGroupInvite)
		r.Get("/groups/requests", api.handleGroupJoinRequests)
		r.Post("/groups/approve", api.handleGroupApprove)
		r.Post("/groups/admin", api.handleGroupAdmin)
		r.Post("/groups/avatar", api.handleGroupAvatar)
		r.Post("/groups/kick", api.handleGroupKick)
		r.Post("/groups/name", api.handleGroupRename)
		r.Post("/groups/settings", api.handleGroupSettings)
		r.Post("/groups/announcement", api.handleGroupAnnouncement)
		r.Post("/groups/announcement/read", api.handleGroupAnnouncementRead)
		r.Post("/groups/dissolve", api.handleGroupDissolve)
		r.Post("/groups/typing", api.handleGroupTyping)
		r.Get("/groups/list", api.handleGroupList)
		r.Get("/groups/members", api.handleGroupMembers)
		r.Post("/groups/message/send", api.handleGroupMessageSend)
		r.Get("/groups/messages/v2", api.handleGroupMessagesV2)
		r.Get("/groups/messages/search", api.handleGroupMessagesSearch)
		r.Get("/groups/messages", api.handleGroupMessages)
		r.Get("/groups/messages/after", api.handleGroupMessagesAfter)
		r.Delete("/groups/messages/{messageID}", api.handleGroupMessageDelete)
		r.Get("/groups/{groupId}/typing", api.handleGroupTypingStatus)
		r.Post("/groups/read", api.handleGroupRead)
		r.Post("/unread/groups", api.handleGroupUnread) // v1: /groups/unread

		// ---- 好友 ----
		r.Get("/friends", api.handleFriendList)
		r.Get("/friends/requests", api.handleFriendRequests)
		r.Post("/friends/request", api.handleFriendRequest)
		r.Post("/friends/respond", api.handleFriendRespond)
		r.Post("/friends/remark", api.handleFriendRemark)
		r.Post("/friends/delete", api.handleFriendDelete)

		// ---- 我的 ----
		r.Get("/me", api.handleMe)
		r.Post("/media", api.handleMediaUpload)
		r.Get("/notifications", api.handleNotificationList)
		r.Post("/me/uid", api.handleUpdateUID)
		r.Post("/me/profile", api.handleUpdateProfile)
		r.Post("/me/avatar", api.handleAvatarUpload)
		r.Post("/me/cover", api.handleCoverUpload)
		r.Post("/me/checkin", api.handleMeCheckIn)
		r.Get("/me/devices", api.handleMeDevices)
		r.Post("/me/devices/cleanup", api.handleMeDevicesCleanupOthers)
		r.Post("/me/password", api.handleUpdatePassword)
		r.Get("/me/group-reports", api.handleMeGroupReports)
		r.Get("/me/bug-reports", api.handleMeBugReports)
		r.Get("/me/user-reports", api.handleMeUserReports)
		r.Post("/me/delete", api.handleDeleteAccount)
		r.Get("/users/profile", api.handleUserProfile)

		// ---- 红包 ----
		r.Post("/redpackets/send", api.handleRedPacketSend)
		r.Post("/redpackets/claim", api.handleRedPacketClaim)
		r.Get("/redpackets/{packetID}", api.handleRedPacketDetail)

		// ---- 朋友圈 ----
		r.Get("/moments", api.handleMomentFeed)
		r.Post("/moments", api.handleMomentCreate)
		r.Post("/moments/like", api.handleMomentLike)
		r.Post("/moments/unlike", api.handleMomentUnlike)
		r.Post("/moments/comment", api.handleMomentComment)
		r.Post("/moments/comment/delete", api.handleMomentCommentDelete)
		r.Get("/moments/comments", api.handleMomentComments)
		r.Post("/moments/delete", api.handleMomentDelete)
		r.Get("/moments/v2", api.handleMomentFeedV2)
		r.Get("/moments/user", api.handleMomentUserFeed)

		// ---- 输入状态 ----
		r.Post("/chats/typing", api.handleChatTyping)
		r.Get("/chats/{chatId}/typing", api.handleChatTypingStatus)
	})
}
