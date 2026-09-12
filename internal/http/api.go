package httpapi

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"

	"metrochat/internal/config"
	"metrochat/internal/data"
	"metrochat/internal/ratelimit"
	"metrochat/internal/secure"
	"metrochat/internal/verify"
	"metrochat/internal/ws"
)

type API struct {
	cfg               config.Config
	dataSyncClient    *http.Client
	db                *sqlx.DB
	users             *data.UserStore
	refresh           *data.RefreshStore
	friendReqs        *data.FriendRequestStore
	friends           *data.FriendStore
	direct            *data.DirectStore
	groups            *data.GroupStore
	groupJoins        *data.GroupJoinStore
	groupMsgs         *data.GroupMessageStore
	groupBans         *data.GroupBanStore
	moments           *data.MomentStore
	resources         *data.ResourceStore
	emojis            *data.EmojiPlazaStore
	musicPlaza        *data.MusicPlazaStore
	favorites         *data.FavoriteStore
	coinTransfers     *data.ExternalCoinTransferStore
	resourceReports   *data.ResourceReportStore
	publicCourt       *data.PublicCourtStore
	banAppeals        *data.BanAppealStore
	devices           *data.DeviceStore
	crashReportStore  *data.CrashReportStore
	reportStore       *data.ReportStore
	groupReportStore  *data.GroupReportStore
	bugReportStore    *data.BugReportStore
	notifications     *data.NotificationStore
	titles            *data.TitleCatalogStore
	wsHub             *ws.Hub
	sessions          *secure.SessionStore
	captchas          *verify.CaptchaStore
	emailCodes        *verify.EmailCodeStore
	sendLimiter       *verify.SendLimiter
	adminSessions     *adminSessions
	ipLimiter         *ratelimit.Limiter
	idLimiter         *ratelimit.Limiter
	typing            *typingStore
	tokenVersionMu    sync.Mutex
	tokenVersionCache map[string]tokenVersionEntry
}

func New(cfg config.Config, db *sqlx.DB) http.Handler {
	// Ensure system user exists for sending notifications
	_ = data.EnsureSystemUser(db)

	api := &API{
		cfg:               cfg,
		dataSyncClient:    &http.Client{Timeout: 70 * time.Second},
		db:                db,
		users:             data.NewUserStore(db),
		refresh:           data.NewRefreshStore(db),
		friendReqs:        data.NewFriendRequestStore(db),
		friends:           data.NewFriendStore(db),
		direct:            data.NewDirectStore(db),
		groups:            data.NewGroupStore(db),
		groupJoins:        data.NewGroupJoinStore(db),
		groupMsgs:         data.NewGroupMessageStore(db),
		groupBans:         data.NewGroupBanStore(db),
		moments:           data.NewMomentStore(db),
		resources:         data.NewResourceStore(db),
		emojis:            data.NewEmojiPlazaStore(db),
		musicPlaza:        data.NewMusicPlazaStore(db),
		favorites:         data.NewFavoriteStore(db),
		coinTransfers:     data.NewExternalCoinTransferStore(db),
		resourceReports:   data.NewResourceReportStore(db),
		publicCourt:       data.NewPublicCourtStore(db),
		banAppeals:        data.NewBanAppealStore(db),
		devices:           data.NewDeviceStore(db),
		crashReportStore:  data.NewCrashReportStore(db),
		reportStore:       data.NewReportStore(db),
		groupReportStore:  data.NewGroupReportStore(db),
		bugReportStore:    data.NewBugReportStore(db),
		notifications:     data.NewNotificationStore(db),
		titles:            data.NewTitleCatalogStore(db),
		wsHub:             ws.NewHub(),
		sessions:          secure.NewSessionStore(),
		captchas:          verify.NewCaptchaStore(),
		emailCodes:        verify.NewEmailCodeStore(),
		sendLimiter:       verify.NewSendLimiter(),
		adminSessions:     newAdminSessions(),
		ipLimiter:         ratelimit.NewLimiter(1.0, 5),
		idLimiter:         ratelimit.NewLimiter(0.2, 3),
		typing:            newTypingStore(),
		tokenVersionCache: make(map[string]tokenVersionEntry),
	}
	setTransferRateLimits(cfg.MediaRateBytes, cfg.UpdateRateBytes, cfg.VideoRateBytes, cfg.MusicRateBytes)
	setTransferConcurrencyLimits(cfg.MediaDownloadConcurrency, cfg.UpdateDownloadConcurrency, cfg.VideoDownloadConcurrency, cfg.MusicDownloadConcurrency)

	// Best-effort table bootstrap so handlers don't do DDL on hot paths.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = api.notifications.EnsureTable(ctx)
	_ = api.bugReportStore.EnsureTable(ctx)
	_ = api.titles.EnsureTable(ctx)
	startMediaCleanup(cfg.UploadDir)

	r := chi.NewRouter()
	r.Use(requestLogger)
	r.Use(recoverer)
	r.Use(gzipResponses)
	r.Use(secureHeaders)

	r.Get("/", api.handleHomePage)
	r.Get("/landing", api.handleHomePage)
	r.Get("/igotbanned", api.handleIGotBannedPage)
	r.Post("/igotbanned", api.handleIGotBannedSubmit)
	r.Get("/app", api.handleWebApp)
	r.Get("/app/login", api.handleWebAppLogin)
	r.Get("/coin-tool", api.handleCoinToolPage)
	if wfs := webappFS(); wfs != nil {
		r.Handle("/app-assets/*", cacheStaticAssets(http.StripPrefix("/app-assets/", http.FileServer(http.FS(wfs)))))
	}
	r.Get("/shop", api.handleLanding)
	r.Get("/shop/report", api.handleReportPage)
	r.Post("/shop/report", api.handleReportSubmit)
	r.Post("/shop/login", api.handleShopLogin)
	r.Get("/shop/logout", api.handleShopLogout)
	r.Get("/v1/music/cover/*", api.handleMusicCoverProxy)
	if lfs := landingAssetsFS(); lfs != nil {
		r.Handle("/landing-assets/*", cacheStaticAssets(http.StripPrefix("/landing-assets/", http.FileServer(http.FS(lfs)))))
	}

	r.Get("/admins", api.handleAdminIndex)
	r.Post("/admins/login", api.handleAdminLogin)
	r.Post("/admins/logout", api.handleAdminLogout)
	r.Post("/admins/ban/device", api.handleAdminBanDevice)
	r.Post("/admins/unban/device", api.handleAdminUnbanDevice)
	r.Post("/admins/ban/user", api.handleAdminBanUser)
	r.Post("/admins/unban/user", api.handleAdminUnbanUser)
	r.Post("/admins/user/deactivate", api.handleAdminDeactivateUser)
	r.Post("/admins/user/delete", api.handleAdminDeleteUser)
	r.Post("/admins/user-title", api.handleAdminUserTitle)
	r.Post("/admins/user-coin", api.handleAdminUserCoin)
	r.Post("/admins/user-uid", api.handleAdminUserUID)
	r.Post("/admins/user/create", api.handleAdminCreateTestUser)
	r.Post("/admins/title/create", api.handleAdminTitleCreate)
	r.Post("/admins/title/update", api.handleAdminTitleUpdate)
	r.Post("/admins/title/toggle", api.handleAdminTitleToggle)
	r.Post("/admins/title/delete", api.handleAdminTitleDelete)
	r.Post("/admins/group/ban", api.handleAdminBanGroup)
	r.Post("/admins/group/unban", api.handleAdminUnbanGroup)
	r.Post("/admins/group/delete", api.handleAdminDeleteGroup)
	r.Post("/admins/group/owner", api.handleAdminSetGroupOwner)
	r.Post("/admins/registration-limit", api.handleAdminRegistrationLimit)
	r.Post("/admins/video-toggle", api.handleAdminVideoToggle)
	r.Get("/admins/crash-reports", api.handleListCrashReports)
	r.Get("/admins/crash-reports/{id}", api.handleGetCrashReport)
	r.Post("/admins/notification/send", api.handleAdminNotificationSend)
	r.Post("/admins/notification/delete", api.handleAdminNotificationDelete)
	r.Get("/admins/bug-reports", api.handleAdminBugReports)
	r.Post("/admins/bug-reports/delete", api.handleAdminBugReportDelete)
	r.Post("/admins/bug-reports/status", api.handleAdminBugReportStatus)
	r.Post("/admins/resources/delete", api.handleAdminResourceDelete)
	r.Post("/admins/emoji-plaza/delete", api.handleAdminEmojiPlazaDelete)
	r.Post("/admins/resource-quota", api.handleAdminResourceQuota)
	r.Post("/admins/bandwidth-limit", api.handleAdminBandwidthLimit)
	r.Post("/admins/data-sync", api.handleAdminDataSyncConfig)
	r.Post("/admins/user-reports/status", api.handleAdminUserReportStatus)
	r.Post("/admins/group-reports/status", api.handleAdminGroupReportStatus)
	r.Post("/admins/resource-reports/status", api.handleAdminResourceReportStatus)
	r.Post("/admins/ban-appeals/status", api.handleAdminBanAppealStatus)
	r.Post("/admins/public-court/close", api.handleAdminPublicCourtClose)
	r.Post("/admins/public-court/clear", api.handleAdminPublicCourtClear)
	r.Get("/admins/server-log", api.handleAdminServerLog)

	r.Handle("/update/*", withUpdateDownloadLimit(http.StripPrefix("/update/", http.FileServer(http.Dir(cfg.UpdateDir)))))
	uploadsHandler := withStaticMediaCache(http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadDir))))
	r.Handle("/uploads/*", withMediaDownloadLimit(func() bool { return api.cfg.VideoEnabled }, uploadsHandler))

	r.Route("/v1", api.registerV1Routes)
	r.Route("/v1/v1", api.registerV1Routes)

	return r
}

func (api *API) registerV1Routes(r chi.Router) {
	r.Use(api.secureMiddleware)
	r.Post("/auth/register", api.handleRegister)
	r.Post("/auth/login", api.handleLogin)
	r.Post("/auth/direct-create", api.handleDirectCreateUser)
	r.Post("/auth/password/reset", api.handleResetPassword)
	r.Post("/auth/handshake", api.handleHandshake)
	r.Get("/auth/captcha", api.handleCaptcha)
	r.Post("/auth/email/send", api.handleEmailCode)
	r.Post("/auth/refresh", api.handleRefresh)
	r.Post("/auth/logout", api.handleLogout)
	r.Get("/ws", api.handleWS)
	r.Get("/music/cover/*", api.handleMusicCoverProxy)
	v1UploadsHandler := withStaticMediaCache(http.StripPrefix("/v1/uploads/", http.FileServer(http.Dir(api.cfg.UploadDir))))
	r.Handle("/uploads/*", withMediaDownloadLimit(func() bool { return api.cfg.VideoEnabled }, v1UploadsHandler))
	r.Post("/external/groups", api.handleExternalGroupList)
	r.Post("/external/friends", api.handleExternalFriendList)
	r.Post("/external/direct/send", api.handleExternalDirectSend)
	r.Post("/external/group/send", api.handleExternalGroupSend)
	r.Post("/external/coin/pay", api.handleExternalCoinPay)
	r.Post("/external/coin/verify", api.handleExternalCoinVerify)

	r.Group(func(r chi.Router) {
		r.Use(api.authMiddleware)
		r.Get("/me", api.handleMe)
		r.Post("/me/checkin", api.handleMeCheckIn)
		r.Get("/me/devices", api.handleMeDevices)
		r.Get("/me/bug-reports", api.handleMeBugReports)
		r.Get("/me/user-reports", api.handleMeUserReports)
		r.Get("/me/group-reports", api.handleMeGroupReports)
		r.Get("/reports/bug", api.handleAllBugReports)
		r.Get("/reports/user", api.handleAllUserReports)
		r.Get("/reports/group", api.handleAllGroupReports)
		r.Post("/me/uid", api.handleUpdateUID)
		r.Post("/me/profile", api.handleUpdateProfile)
		r.Post("/me/password", api.handleUpdatePassword)
		r.Post("/me/delete", api.handleDeleteAccount)
		r.Post("/me/avatar", api.handleAvatarUpload)
		r.Post("/me/cover", api.handleCoverUpload)
		r.Post("/media", api.handleMediaUpload)
		r.Get("/users/profile", api.handleUserProfile)
		r.Get("/friends", api.handleFriendList)
		r.Get("/friends/requests", api.handleFriendRequests)
		r.Post("/friends/request", api.handleFriendRequest)
		r.Post("/friends/respond", api.handleFriendRespond)
		r.Post("/friends/remark", api.handleFriendRemark)
		r.Post("/friends/delete", api.handleFriendDelete)
		r.Post("/direct/send", api.handleDirectSend)
		r.Post("/chats/typing", api.handleChatTyping)
		r.Get("/chats/{chatId}/typing", api.handleChatTypingStatus)
		r.Post("/redpackets/send", api.handleRedPacketSend)
		r.Post("/redpackets/claim", api.handleRedPacketClaim)
		r.Get("/redpackets/{packetID}", api.handleRedPacketDetail)
		r.Get("/direct/messages/v2", api.handleDirectMessagesV2)
		r.Get("/direct/messages/search", api.handleDirectMessagesSearch)
		r.Get("/direct/messages", api.handleDirectMessages)
		r.Post("/direct/unread", api.handleDirectUnread)
		r.Post("/direct/read", api.handleDirectRead)
		r.Delete("/direct/messages/{messageID}", api.handleDirectMessageDelete)
		r.Post("/groups/create", api.handleGroupCreate)
		r.Post("/groups/join", api.handleGroupJoin)
		r.Post("/groups/approve", api.handleGroupApprove)
		r.Get("/groups/list", api.handleGroupList)
		r.Get("/groups/members", api.handleGroupMembers)
		r.Get("/groups/requests", api.handleGroupJoinRequests)
		r.Post("/groups/invite", api.handleGroupInvite)
		r.Post("/groups/admin", api.handleGroupAdmin)
		r.Post("/groups/avatar", api.handleGroupAvatar)
		r.Post("/groups/kick", api.handleGroupKick)
		r.Post("/groups/name", api.handleGroupRename)
		r.Post("/groups/settings", api.handleGroupSettings)
		r.Post("/groups/announcement", api.handleGroupAnnouncement)
		r.Post("/groups/announcement/read", api.handleGroupAnnouncementRead)
		r.Post("/groups/leave", api.handleGroupLeave)
		r.Post("/groups/dissolve", api.handleGroupDissolve)
		r.Post("/groups/typing", api.handleGroupTyping)
		r.Get("/groups/{groupId}/typing", api.handleGroupTypingStatus)
		r.Post("/groups/message/send", api.handleGroupMessageSend)
		r.Post("/groups/unread", api.handleGroupUnread)
		r.Post("/groups/read", api.handleGroupRead)
		r.Get("/groups/messages/v2", api.handleGroupMessagesV2)
		r.Get("/groups/messages/search", api.handleGroupMessagesSearch)
		r.Get("/groups/messages", api.handleGroupMessages)
		r.Delete("/groups/messages/{messageID}", api.handleGroupMessageDelete)
		r.Post("/moments", api.handleMomentCreate)
		r.Get("/moments", api.handleMomentFeed)
		r.Get("/moments/v2", api.handleMomentFeedV2)
		r.Get("/moments/user", api.handleMomentUserFeed)
		r.Post("/moments/like", api.handleMomentLike)
		r.Post("/moments/unlike", api.handleMomentUnlike)
		r.Post("/moments/delete", api.handleMomentDelete)
		r.Post("/moments/comment", api.handleMomentComment)
		r.Post("/moments/comment/delete", api.handleMomentCommentDelete)
		r.Get("/moments/comments", api.handleMomentComments)
		r.Get("/emoji/plaza", api.handleEmojiPlazaList)
		r.Get("/emoji/plaza/mine", api.handleEmojiPlazaMineList)
		r.Post("/emoji/plaza/upload", api.handleEmojiPlazaUpload)
		r.Post("/emoji/plaza/save", api.handleEmojiPlazaSave)
		r.Post("/emoji/plaza/delete", api.handleEmojiPlazaDelete)
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
		r.Get("/favorites", api.handleFavoriteList)
		r.Post("/favorites/add", api.handleFavoriteAdd)
		r.Post("/favorites/remove", api.handleFavoriteRemove)
		r.Post("/reports/user", api.handleUserReport)
		r.Get("/public-court/cases", api.handlePublicCourtCases)
		r.Get("/public-court/cases/{caseID}", api.handlePublicCourtCaseDetail)
		r.Get("/public-court/cases/{caseID}/votes", api.handlePublicCourtCaseVotes)
		r.Get("/public-court/cases/{caseID}/discussions", api.handlePublicCourtCaseDiscussions)
		r.Post("/public-court/cases/{caseID}/vote", api.handlePublicCourtCaseVote)
		r.Post("/public-court/cases/{caseID}/statement", api.handlePublicCourtCaseStatement)
		r.Post("/public-court/cases/{caseID}/discussion", api.handlePublicCourtCaseDiscussion)
		r.Post("/public-court/cases/{caseID}/withdraw", api.handlePublicCourtCaseWithdraw)
		r.Post("/reports/group", api.handleGroupReport)
		r.Post("/admins/crash-reports", api.handleSubmitCrashReport)
		r.Post("/feedback", api.handleSubmitBugReport)
		r.Get("/notifications", api.handleNotificationList)
	})
}
