package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// v1 全量保留兼容旧客户端；v2 复用同一批 handler，差异只在中间件层。
func (api *API) registerV2Routes(r chi.Router) {
	r.Use(api.secureMiddleware)
	r.Use(api.v2SignMiddleware)

	r.Post("/gateway", api.handleV2Gateway)

	// ---- 认证（无需 token，与 v1 同名同义）----
	r.Post("/auth/register", api.handleRegister)
	r.Post("/auth/login", api.handleLogin)
	r.Post("/auth/logout", api.handleLogout)
	r.Post("/auth/refresh", api.handleRefresh)
	r.Post("/auth/handshake", api.handleHandshake)
	r.Get("/auth/captcha", api.handleCaptcha)
	r.Post("/auth/email/send", api.handleEmailCode)
	r.Post("/auth/password/reset", api.handleResetPassword)
	r.Post("/auth/direct-create", api.handleDirectCreateUser)

	// ---- 音乐封面代理（v1 路径 /v1/music/cover/*，此处同步暴露）----
	r.Get("/music/cover/*", api.handleMusicCoverProxy)

	// 刻意不暴露 /v2/ws：客户端 SDK 固定连 /v1/ws（见 oldchat-ws-extension.js），
	// 且 WS 握手无法携带逐帧 X-Sign，走 v2SignMiddleware 必然 401。

	// v2 下的静态媒体，与 v1/uploads 同源
	v2UploadsHandler := withStaticMediaCache(http.StripPrefix("/v2/uploads/", http.FileServer(http.Dir(api.cfg.UploadDir))))
	r.Handle("/uploads/*", withMediaDownloadLimit(func() bool { return api.cfg.VideoEnabled }, v2UploadsHandler))

	// ---- 第三方对接（自带 external 鉴权，不走 Bearer）----
	r.Post("/external/groups", api.handleExternalGroupList)
	r.Post("/external/friends", api.handleExternalFriendList)
	r.Post("/external/direct/send", api.handleExternalDirectSend)
	r.Post("/external/group/send", api.handleExternalGroupSend)
	r.Post("/external/coin/pay", api.handleExternalCoinPay)
	r.Post("/external/coin/verify", api.handleExternalCoinVerify)

	r.Group(func(r chi.Router) {
		r.Use(api.authMiddleware)

		// ---- 事件差量 ----
		r.Get("/updates/difference", api.handleUpdatesDifference)
		r.Get("/groups/events/after", api.handleGroupEventsAfter)

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
		r.Get("/groups/invitations", api.handleGroupInvitations)
		r.Post("/groups/invitations/respond", api.handleGroupInvitationRespond)
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
		r.Get("/groups/members/lookup", api.handleGroupMembersLookup)
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
		r.Post("/me/presence", api.handleMePresence)
		r.Get("/me/scratch", api.handleMeScratchGet)
		r.Post("/me/scratch", api.handleMeScratchPost)
		r.Get("/me/group-invite-preference", api.handleMeGroupInvitePreferenceGet)
		r.Post("/me/group-invite-preference", api.handleMeGroupInvitePreferenceSet)
		r.Get("/users/profile", api.handleUserProfile)

		// ---- 红包 ----
		r.Post("/redpackets/send", api.handleRedPacketSend)
		r.Post("/redpackets/claim", api.handleRedPacketClaim)
		r.Get("/redpackets/{packetID}", api.handleRedPacketDetail)

		// ---- 朋友圈 ----
		r.Get("/moments", api.handleMomentFeed)
		// 官方规范路径，语义同 /moments（游标分页 before_created_at）
		r.Get("/moments/feed", api.handleMomentFeed)
		r.Post("/moments", api.handleMomentCreate)
		r.Post("/moments/like", api.handleMomentLike)
		r.Post("/moments/unlike", api.handleMomentUnlike)
		r.Post("/moments/comment", api.handleMomentComment)
		r.Post("/moments/comment/delete", api.handleMomentCommentDelete)
		r.Get("/moments/comments", api.handleMomentComments)
		r.Post("/moments/delete", api.handleMomentDelete)
		r.Get("/moments/v2", api.handleMomentFeedV2)
		r.Get("/moments/user", api.handleMomentUserFeed)

		// ---- 文件资产 / 资源下载 ----
		// 这批是免签名豁免路径（v2SignMiddleware 按 v2UnsignedPaths 放行），
		// 只靠下面的 authMiddleware 校验 Bearer JWT。
		r.Post("/files/check", api.handleFileCheck)
		r.Post("/files/upload", api.handleFileUpload)
		r.Get("/files/download/{fileID}", api.handleFileDownload)
		r.Head("/files/download/{fileID}", api.handleFileDownload)
		r.Post("/resources/upload", api.handleResourceUploadV2)
		r.Get("/resources/download/{itemID}", api.handleResourceDownload)
		r.Head("/resources/download/{itemID}", api.handleResourceDownload)

		// ---- 输入状态 ----
		r.Post("/chats/typing", api.handleChatTyping)
		r.Get("/chats/{chatId}/typing", api.handleChatTypingStatus)

		// ---- 以下为 v1 形态原样暴露，handler 无需改动（无协议差异）----

		// v1 别名：客户端 v2 侧仍可能叫 /direct/unread、/groups/unread
		r.Post("/direct/unread", api.handleDirectUnread)
		r.Post("/groups/unread", api.handleGroupUnread)

		// 收藏
		r.Get("/favorites", api.handleFavoriteList)
		r.Post("/favorites/add", api.handleFavoriteAdd)
		r.Post("/favorites/remove", api.handleFavoriteRemove)

		// 表情广场
		r.Get("/emoji/plaza", api.handleEmojiPlazaList)
		r.Get("/emoji/plaza/mine", api.handleEmojiPlazaMineList)
		r.Post("/emoji/plaza/upload", api.handleEmojiPlazaUpload)
		r.Post("/emoji/plaza/save", api.handleEmojiPlazaSave)
		r.Post("/emoji/plaza/delete", api.handleEmojiPlazaDelete)

		// 音乐广场
		r.Get("/music/plaza", api.handleMusicPlazaList)
		r.Get("/music/plaza/mine", api.handleMusicPlazaMineList)
		r.Post("/music/plaza/upload", api.handleMusicPlazaUpload)
		r.Post("/music/plaza/lyrics", api.handleMusicPlazaLyricsUpload)
		r.Post("/music/plaza/delete", api.handleMusicPlazaDelete)
		r.Post("/music/plaza/mine/delete-batch", api.handleMusicPlazaMineBatchDelete)
		r.Post("/music/plaza/like", api.handleMusicPlazaLike)
		r.Post("/music/plaza/unlike", api.handleMusicPlazaUnlike)
		r.Post("/music/plaza/comment", api.handleMusicPlazaComment)
		r.Post("/music/plaza/comment/delete", api.handleMusicPlazaCommentDelete)
		r.Get("/music/plaza/comments", api.handleMusicPlazaComments)
		r.Post("/music/plaza/play", api.handleMusicPlazaPlay)
		r.Get("/music/plaza/ranking", api.handleMusicPlazaRanking)

		// 举报
		r.Get("/reports/bug", api.handleAllBugReports)
		r.Get("/reports/user", api.handleAllUserReports)
		r.Get("/reports/group", api.handleAllGroupReports)
		r.Post("/reports/user", api.handleUserReport)
		r.Post("/reports/group", api.handleGroupReport)
		r.Post("/feedback", api.handleSubmitBugReport)
		r.Post("/admins/crash-reports", api.handleSubmitCrashReport)

		// 公审台
		r.Get("/public-court/cases", api.handlePublicCourtCases)
		r.Get("/public-court/cases/{caseID}", api.handlePublicCourtCaseDetail)
		r.Get("/public-court/cases/{caseID}/votes", api.handlePublicCourtCaseVotes)
		r.Get("/public-court/cases/{caseID}/discussions", api.handlePublicCourtCaseDiscussions)
		r.Post("/public-court/cases/{caseID}/vote", api.handlePublicCourtCaseVote)
		r.Post("/public-court/cases/{caseID}/statement", api.handlePublicCourtCaseStatement)
		r.Post("/public-court/cases/{caseID}/discussion", api.handlePublicCourtCaseDiscussion)
		r.Post("/public-court/cases/{caseID}/withdraw", api.handlePublicCourtCaseWithdraw)
	})
}
