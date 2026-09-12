package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrPublicCourtClosed = errors.New("public court case closed")
var ErrPublicCourtForbidden = errors.New("public court forbidden")
var ErrPublicCourtInvalidVote = errors.New("public court invalid vote")
var ErrPublicCourtVoteEvidenceRequired = errors.New("public court vote evidence required")
var ErrPublicCourtReputationTooLow = errors.New("public court reputation too low")
var ErrPublicCourtInvalidVerdict = errors.New("public court invalid verdict")
var ErrPublicCourtAlreadyClosed = errors.New("public court already closed")
var ErrPublicCourtInvalidStatement = errors.New("public court invalid statement")
var ErrPublicCourtInvalidDiscussion = errors.New("public court invalid discussion")

const (
	publicCourtVoteLockThreshold   = 10
	publicCourtAutoBanHours        = 24
	publicCourtVoteMinReputation   = 50
	publicCourtMajorReputationGain = 50
	publicCourtMinorReputationLose = 100
	publicCourtWithdrawPenalty     = 50
)

type PublicCourtCase struct {
	ID              string       `db:"id"`
	ReportID        string       `db:"report_id"`
	ReporterID      string       `db:"reporter_id"`
	ReporterUID     string       `db:"reporter_uid"`
	DefendantID     string       `db:"defendant_id"`
	DefendantUID    string       `db:"defendant_uid"`
	ReportReason    string       `db:"report_reason"`
	ReportEvidence  string       `db:"report_evidence"`
	DefenseReason   string       `db:"defense_reason"`
	DefenseEvidence string       `db:"defense_evidence"`
	Status          string       `db:"status"`
	Verdict         string       `db:"verdict"`
	AdminNote       string       `db:"admin_note"`
	BanHours        int          `db:"ban_hours"`
	RewardProcessed int          `db:"reward_processed"`
	ClosedAt        sql.NullTime `db:"closed_at"`
	CreatedAt       time.Time    `db:"created_at"`
	UpdatedAt       time.Time    `db:"updated_at"`
}

type PublicCourtCaseSummary struct {
	PublicCourtCase
	ReporterName    string `db:"reporter_name"`
	ReporterAvatar  string `db:"reporter_avatar"`
	DefendantName   string `db:"defendant_name"`
	DefendantAvatar string `db:"defendant_avatar"`
	BanVoteCount    int    `db:"ban_vote_count"`
	KeepVoteCount   int    `db:"keep_vote_count"`
	TotalVoteCount  int    `db:"total_vote_count"`
	MyVote          string `db:"my_vote"`
	MyVoteReason    string `db:"my_vote_reason"`
}

type PublicCourtVote struct {
	CaseID      string    `db:"case_id"`
	VoterID     string    `db:"voter_id"`
	VoterUID    string    `db:"voter_uid"`
	VoterName   string    `db:"voter_name"`
	VoterAvatar string    `db:"voter_avatar"`
	Vote        string    `db:"vote"`
	Reason      string    `db:"reason"`
	Evidence    string    `db:"evidence"`
	CreatedAt   time.Time `db:"created_at"`
}

type PublicCourtStatement struct {
	CaseID     string    `db:"case_id"`
	UserID     string    `db:"user_id"`
	UserUID    string    `db:"user_uid"`
	UserName   string    `db:"user_name"`
	UserAvatar string    `db:"user_avatar"`
	Role       string    `db:"role"`
	Reason     string    `db:"reason"`
	Evidence   string    `db:"evidence"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

type PublicCourtDiscussion struct {
	ID         string    `db:"id"`
	CaseID     string    `db:"case_id"`
	UserID     string    `db:"user_id"`
	UserUID    string    `db:"user_uid"`
	UserName   string    `db:"user_name"`
	UserAvatar string    `db:"user_avatar"`
	Body       string    `db:"body"`
	CreatedAt  time.Time `db:"created_at"`
}

type PublicCourtMergedReport struct {
	ReportID       string    `db:"report_id"`
	ReporterUID    string    `db:"reporter_uid"`
	ReporterName   string    `db:"reporter_name"`
	ReporterAvatar string    `db:"reporter_avatar"`
	Reason         string    `db:"reason"`
	CreatedAt      time.Time `db:"created_at"`
}

type PublicCourtVoteResult struct {
	CaseID            string
	CaseStatus        string
	DefendantID       string
	BanVoteCount      int
	KeepVoteCount     int
	TotalVoteCount    int
	LockedForReview   bool
	JuryVerdict       string
	TemporaryBanHours int
}

type PublicCourtStore struct {
	db *sqlx.DB
}

func NewPublicCourtStore(db *sqlx.DB) *PublicCourtStore {
	return &PublicCourtStore{db: db}
}

func (s *PublicCourtStore) GetByID(ctx context.Context, caseID string) (*PublicCourtCase, error) {
	if caseID == "" {
		return nil, ErrNotFound
	}
	var item PublicCourtCase
	err := s.db.GetContext(ctx, &item, `
SELECT id, report_id, reporter_id, reporter_uid, defendant_id, defendant_uid,
       report_reason, report_evidence, defense_reason, defense_evidence,
       status, verdict, admin_note, ban_hours, reward_processed,
       closed_at, created_at, updated_at
FROM public_court_cases
WHERE id = $1
`, caseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *PublicCourtStore) FindOpenByPair(ctx context.Context, reporterID, defendantID string) (*PublicCourtCase, error) {
	if reporterID == "" || defendantID == "" {
		return nil, ErrNotFound
	}
	var item PublicCourtCase
	err := s.db.GetContext(ctx, &item, `
SELECT id, report_id, reporter_id, reporter_uid, defendant_id, defendant_uid,
       report_reason, report_evidence, defense_reason, defense_evidence,
       status, verdict, admin_note, ban_hours, reward_processed,
       closed_at, created_at, updated_at
FROM public_court_cases
WHERE reporter_id = $1 AND defendant_id = $2 AND status = 'open'
ORDER BY created_at DESC
LIMIT 1
`, reporterID, defendantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *PublicCourtStore) ListActiveByDefendant(ctx context.Context, defendantID string, limit int) ([]PublicCourtCase, error) {
	if defendantID == "" {
		return []PublicCourtCase{}, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows := make([]PublicCourtCase, 0)
	err := s.db.SelectContext(ctx, &rows, `
SELECT id, report_id, reporter_id, reporter_uid, defendant_id, defendant_uid,
       report_reason, report_evidence, defense_reason, defense_evidence,
       status, verdict, admin_note, ban_hours, reward_processed,
       closed_at, created_at, updated_at
FROM public_court_cases
WHERE defendant_id = $1 AND status IN ('open', 'pending_review')
ORDER BY created_at DESC
LIMIT $2
`, defendantID, limit)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func stripMergedReportLabel(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "【已叠加 ") {
		return t
	}
	idx := strings.Index(t, "\n")
	if idx <= 0 {
		return ""
	}
	head := strings.TrimSpace(t[:idx])
	if strings.HasSuffix(head, " 条举报】") {
		return strings.TrimSpace(t[idx+1:])
	}
	return t
}

func mergeReportText(existing, incoming string) string {
	oldText := stripMergedReportLabel(existing)
	newText := strings.TrimSpace(incoming)
	if newText == "" {
		return oldText
	}
	if oldText == "" {
		return newText
	}
	if strings.Contains(oldText, newText) {
		return oldText
	}
	return oldText + "\n\n" + newText
}

func parseMergedReportIDs(reportIDs string) []string {
	ids := strings.Split(strings.TrimSpace(reportIDs), ",")
	seen := make(map[string]struct{})
	items := make([]string, 0, len(ids))
	for i := 0; i < len(ids); i++ {
		id := strings.TrimSpace(ids[i])
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		items = append(items, id)
	}
	return items
}

func countMergedReportIDs(reportIDs string) int {
	items := parseMergedReportIDs(reportIDs)
	if len(items) == 0 {
		return 1
	}
	return len(items)
}

func addMergedReportLabel(reason string, mergedCount int) string {
	cleaned := stripMergedReportLabel(reason)
	if mergedCount <= 1 || cleaned == "" {
		return cleaned
	}
	return fmt.Sprintf("【已叠加 %d 条举报】\n%s", mergedCount, cleaned)
}

func mergeReportID(existing, incoming string) string {
	oldID := strings.TrimSpace(existing)
	newID := strings.TrimSpace(incoming)
	if newID == "" {
		return oldID
	}
	if oldID == "" {
		return newID
	}
	if oldID == newID {
		return oldID
	}
	if strings.Contains(oldID, newID) {
		return oldID
	}
	return oldID + "," + newID
}

func (s *PublicCourtStore) mergeIntoCase(ctx context.Context, existing *PublicCourtCase, item *PublicCourtCase) (*PublicCourtCase, error) {
	if existing == nil || item == nil {
		return nil, ErrNotFound
	}
	mergedReportID := mergeReportID(existing.ReportID, item.ReportID)
	mergedCount := countMergedReportIDs(mergedReportID)
	mergedReason := addMergedReportLabel(mergeReportText(existing.ReportReason, item.ReportReason), mergedCount)
	mergedEvidence := mergeReportText(existing.ReportEvidence, item.ReportEvidence)

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `
UPDATE public_court_cases
SET report_id = $1,
    report_reason = $2,
    report_evidence = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $4
`, mergedReportID, mergedReason, mergedEvidence, existing.ID)
	if err != nil {
		return nil, err
	}

	reason := strings.TrimSpace(item.ReportReason)
	evidence := strings.TrimSpace(item.ReportEvidence)
	if item.ReporterID != "" && (reason != "" || evidence != "") {
		_, err = tx.ExecContext(ctx, `
INSERT INTO public_court_statements (
    case_id, user_id, role, reason, evidence, created_at, updated_at
) VALUES (
    $1, $2, 'reporter', $3, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
ON CONFLICT(case_id, user_id)
DO UPDATE SET role = 'reporter',
              reason = excluded.reason,
              evidence = excluded.evidence,
              updated_at = CURRENT_TIMESTAMP
`, existing.ID, item.ReporterID, reason, evidence)
		if err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil

	return s.GetByID(ctx, existing.ID)
}

func (s *PublicCourtStore) OpenOrCreate(ctx context.Context, item *PublicCourtCase) (*PublicCourtCase, bool, error) {
	if item == nil || item.ReporterID == "" || item.DefendantID == "" {
		return nil, false, ErrNotFound
	}
	activeCases, activeErr := s.ListActiveByDefendant(ctx, item.DefendantID, 10)
	if activeErr != nil {
		return nil, false, activeErr
	}
	if len(activeCases) == 1 {
		merged, err := s.mergeIntoCase(ctx, &activeCases[0], item)
		if err != nil {
			return nil, false, err
		}
		return merged, false, nil
	}
	if len(activeCases) > 1 {
		for i := 0; i < len(activeCases); i++ {
			if activeCases[i].ReporterID == item.ReporterID {
				merged, err := s.mergeIntoCase(ctx, &activeCases[i], item)
				if err != nil {
					return nil, false, err
				}
				return merged, false, nil
			}
		}
	}

	existing, err := s.FindOpenByPair(ctx, item.ReporterID, item.DefendantID)
	if err == nil {
		merged, mergeErr := s.mergeIntoCase(ctx, existing, item)
		if mergeErr == nil {
			return merged, false, nil
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	if item.ID == "" {
		item.ID = NewID()
	}
	if item.Status == "" {
		item.Status = "open"
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO public_court_cases (
    id, report_id, reporter_id, reporter_uid, defendant_id, defendant_uid,
    report_reason, report_evidence, defense_reason, defense_evidence,
    status, verdict, admin_note, ban_hours, reward_processed,
    created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13, $14, $15,
    CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
`, item.ID, item.ReportID, item.ReporterID, item.ReporterUID, item.DefendantID,
		item.DefendantUID, item.ReportReason, item.ReportEvidence, item.DefenseReason,
		item.DefenseEvidence, item.Status, item.Verdict, item.AdminNote, item.BanHours,
		item.RewardProcessed)
	if err != nil {
		return nil, false, err
	}
	created, err := s.GetByID(ctx, item.ID)
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}

func (s *PublicCourtStore) ListCases(ctx context.Context, status string, limit int, before time.Time, viewerID string) ([]PublicCourtCaseSummary, error) {
	if limit <= 0 || limit > 500 {
		limit = 30
	}
	status = normalizePublicCourtStatus(status)

	query := `
SELECT c.id, c.report_id, c.reporter_id, c.reporter_uid, c.defendant_id, c.defendant_uid,
       c.report_reason, c.report_evidence, c.defense_reason, c.defense_evidence,
       c.status, c.verdict, c.admin_note, c.ban_hours, c.reward_processed,
       COALESCE(NULLIF(ru.display_name, ''), ru.username, '') AS reporter_name,
       COALESCE(ru.avatar_url, '') AS reporter_avatar,
       COALESCE(NULLIF(du.display_name, ''), du.username, '') AS defendant_name,
       COALESCE(du.avatar_url, '') AS defendant_avatar,
       c.closed_at, c.created_at, c.updated_at,
       COALESCE(vs.ban_vote_count, 0) AS ban_vote_count,
       COALESCE(vs.keep_vote_count, 0) AS keep_vote_count,
       COALESCE(vs.total_vote_count, 0) AS total_vote_count,
       COALESCE(mv.vote, '') AS my_vote,
       COALESCE(mv.reason, '') AS my_vote_reason
FROM public_court_cases c
LEFT JOIN users ru ON ru.id = c.reporter_id
LEFT JOIN users du ON du.id = c.defendant_id
LEFT JOIN (
    SELECT case_id,
           SUM(CASE WHEN vote = 'ban' THEN 1 ELSE 0 END) AS ban_vote_count,
           SUM(CASE WHEN vote = 'keep' THEN 1 ELSE 0 END) AS keep_vote_count,
           COUNT(1) AS total_vote_count
    FROM public_court_votes
    GROUP BY case_id
) vs ON vs.case_id = c.id
LEFT JOIN public_court_votes mv ON mv.case_id = c.id AND mv.voter_id = $1
`
	args := []interface{}{viewerID}
	where := []string{"1=1"}
	if status == "open" || status == "closed" || status == "pending_review" || status == "withdrawn" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("c.status = $%d", len(args)))
	}
	if !before.IsZero() {
		args = append(args, before.UTC().Format("2006-01-02 15:04:05"))
		where = append(where, fmt.Sprintf("c.created_at < $%d", len(args)))
	}
	args = append(args, limit)
	query = query + " WHERE " + strings.Join(where, " AND ") +
		fmt.Sprintf(" ORDER BY c.created_at DESC LIMIT $%d", len(args))

	rows := make([]PublicCourtCaseSummary, 0)
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *PublicCourtStore) GetCaseSummary(ctx context.Context, caseID, viewerID string) (*PublicCourtCaseSummary, error) {
	if caseID == "" {
		return nil, ErrNotFound
	}
	var item PublicCourtCaseSummary
	err := s.db.GetContext(ctx, &item, `
SELECT c.id, c.report_id, c.reporter_id, c.reporter_uid, c.defendant_id, c.defendant_uid,
       c.report_reason, c.report_evidence, c.defense_reason, c.defense_evidence,
       c.status, c.verdict, c.admin_note, c.ban_hours, c.reward_processed,
       COALESCE(NULLIF(ru.display_name, ''), ru.username, '') AS reporter_name,
       COALESCE(ru.avatar_url, '') AS reporter_avatar,
       COALESCE(NULLIF(du.display_name, ''), du.username, '') AS defendant_name,
       COALESCE(du.avatar_url, '') AS defendant_avatar,
       c.closed_at, c.created_at, c.updated_at,
       COALESCE(vs.ban_vote_count, 0) AS ban_vote_count,
       COALESCE(vs.keep_vote_count, 0) AS keep_vote_count,
       COALESCE(vs.total_vote_count, 0) AS total_vote_count,
       COALESCE(mv.vote, '') AS my_vote,
       COALESCE(mv.reason, '') AS my_vote_reason
FROM public_court_cases c
LEFT JOIN users ru ON ru.id = c.reporter_id
LEFT JOIN users du ON du.id = c.defendant_id
LEFT JOIN (
    SELECT case_id,
           SUM(CASE WHEN vote = 'ban' THEN 1 ELSE 0 END) AS ban_vote_count,
           SUM(CASE WHEN vote = 'keep' THEN 1 ELSE 0 END) AS keep_vote_count,
           COUNT(1) AS total_vote_count
    FROM public_court_votes
    GROUP BY case_id
) vs ON vs.case_id = c.id
LEFT JOIN public_court_votes mv ON mv.case_id = c.id AND mv.voter_id = $1
WHERE c.id = $2
`, viewerID, caseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *PublicCourtStore) ListRecentVotes(ctx context.Context, caseID string, limit int) ([]PublicCourtVote, error) {
	if caseID == "" {
		return []PublicCourtVote{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows := make([]PublicCourtVote, 0)
	err := s.db.SelectContext(ctx, &rows, `
SELECT v.case_id, v.voter_id, COALESCE(u.uid, '') AS voter_uid,
       COALESCE(NULLIF(u.display_name, ''), u.username, '') AS voter_name,
       COALESCE(u.avatar_url, '') AS voter_avatar,
       v.vote, v.reason, v.evidence, v.created_at
FROM public_court_votes v
LEFT JOIN users u ON u.id = v.voter_id
WHERE v.case_id = $1
ORDER BY v.created_at DESC
LIMIT $2
`, caseID, limit)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *PublicCourtStore) SaveStatement(ctx context.Context, caseID, userID, reason, evidence string) (string, error) {
	if caseID == "" || userID == "" {
		return "", ErrNotFound
	}
	reason = strings.TrimSpace(reason)
	evidence = strings.TrimSpace(evidence)
	if reason == "" && evidence == "" {
		return "", ErrPublicCourtInvalidStatement
	}
	item, err := s.GetByID(ctx, caseID)
	if err != nil {
		return "", err
	}
	if item.Status != "open" {
		return "", ErrPublicCourtClosed
	}
	role := ""
	switch userID {
	case item.ReporterID:
		role = "reporter"
	case item.DefendantID:
		role = "defendant"
	default:
		var voted int
		checkErr := s.db.GetContext(ctx, &voted, `
SELECT 1
FROM public_court_votes
WHERE case_id = $1 AND voter_id = $2
LIMIT 1
`, caseID, userID)
		if checkErr != nil {
			if errors.Is(checkErr, sql.ErrNoRows) {
				return "", ErrPublicCourtForbidden
			}
			return "", checkErr
		}
		role = "jury"
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO public_court_statements (
    case_id, user_id, role, reason, evidence, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
ON CONFLICT(case_id, user_id)
DO UPDATE SET
    role = excluded.role,
    reason = excluded.reason,
    evidence = excluded.evidence,
    updated_at = CURRENT_TIMESTAMP
`, caseID, userID, role, reason, evidence)
	if err != nil {
		return "", err
	}

	if role == "reporter" {
		_, err = s.db.ExecContext(ctx, `
UPDATE public_court_cases
SET report_reason = $1,
    report_evidence = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $3
`, reason, evidence, caseID)
		if err != nil {
			return "", err
		}
		return role, nil
	}
	if role == "defendant" {
		_, err = s.db.ExecContext(ctx, `
UPDATE public_court_cases
SET defense_reason = $1,
    defense_evidence = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $3
`, reason, evidence, caseID)
		if err != nil {
			return "", err
		}
		return role, nil
	}
	return role, nil
}

func (s *PublicCourtStore) ListMergedReports(ctx context.Context, caseID string, page int, pageSize int) ([]PublicCourtMergedReport, int, error) {
	if caseID == "" {
		return []PublicCourtMergedReport{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}

	var reportIDsRaw string
	err := s.db.GetContext(ctx, &reportIDsRaw, `
SELECT COALESCE(report_id, '')
FROM public_court_cases
WHERE id = $1
`, caseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []PublicCourtMergedReport{}, 0, ErrNotFound
		}
		return nil, 0, err
	}

	reportIDs := parseMergedReportIDs(reportIDsRaw)
	if len(reportIDs) == 0 {
		return []PublicCourtMergedReport{}, 0, nil
	}

	countQuery, countArgs, err := sqlx.In(`
SELECT COUNT(1)
FROM user_reports
WHERE id IN (?)
`, reportIDs)
	if err != nil {
		return nil, 0, err
	}
	countQuery = s.db.Rebind(countQuery)
	var total int
	if err := s.db.GetContext(ctx, &total, countQuery, countArgs...); err != nil {
		return nil, 0, err
	}
	if total <= 0 {
		return []PublicCourtMergedReport{}, 0, nil
	}

	offset := (page - 1) * pageSize
	if offset >= total {
		offset = ((total - 1) / pageSize) * pageSize
		if offset < 0 {
			offset = 0
		}
	}

	query, args, err := sqlx.In(`
SELECT r.id AS report_id,
       COALESCE(r.reporter_uid, '') AS reporter_uid,
       COALESCE(NULLIF(u.display_name, ''), u.username, r.reporter_uid, '') AS reporter_name,
       COALESCE(u.avatar_url, '') AS reporter_avatar,
       COALESCE(r.reason, '') AS reason,
       r.created_at
FROM user_reports r
LEFT JOIN users u ON u.id = r.reporter_id
WHERE r.id IN (?)
ORDER BY r.created_at DESC
LIMIT ? OFFSET ?
`, reportIDs, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	query = s.db.Rebind(query)

	rows := make([]PublicCourtMergedReport, 0)
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *PublicCourtStore) ListStatements(ctx context.Context, caseID string, limit int) ([]PublicCourtStatement, error) {
	if caseID == "" {
		return []PublicCourtStatement{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows := make([]PublicCourtStatement, 0)
	err := s.db.SelectContext(ctx, &rows, `
SELECT s.case_id, s.user_id, COALESCE(u.uid, '') AS user_uid,
       COALESCE(NULLIF(u.display_name, ''), u.username, '') AS user_name,
       COALESCE(u.avatar_url, '') AS user_avatar,
       s.role, s.reason, s.evidence, s.created_at, s.updated_at
FROM public_court_statements s
LEFT JOIN users u ON u.id = s.user_id
WHERE s.case_id = $1
ORDER BY s.updated_at DESC, s.created_at DESC
LIMIT $2
`, caseID, limit)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *PublicCourtStore) ListDiscussions(ctx context.Context, caseID string, limit int) ([]PublicCourtDiscussion, error) {
	if caseID == "" {
		return []PublicCourtDiscussion{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows := make([]PublicCourtDiscussion, 0)
	err := s.db.SelectContext(ctx, &rows, `
SELECT d.id, d.case_id, d.user_id,
       COALESCE(u.uid, '') AS user_uid,
       COALESCE(NULLIF(u.display_name, ''), u.username, '') AS user_name,
       COALESCE(u.avatar_url, '') AS user_avatar,
       d.body, d.created_at
FROM public_court_discussions d
LEFT JOIN users u ON u.id = d.user_id
WHERE d.case_id = $1
ORDER BY d.created_at DESC
LIMIT $2
`, caseID, limit)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *PublicCourtStore) AddDiscussion(ctx context.Context, caseID, userID, body string) (*PublicCourtDiscussion, error) {
	if caseID == "" || userID == "" {
		return nil, ErrNotFound
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, ErrPublicCourtInvalidDiscussion
	}
	if len([]rune(body)) > 800 {
		return nil, ErrPublicCourtInvalidDiscussion
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var caseStatus string
	err = tx.GetContext(ctx, &caseStatus, `
SELECT status
FROM public_court_cases
WHERE id = $1
`, caseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if caseStatus == "closed" || caseStatus == "withdrawn" {
		return nil, ErrPublicCourtClosed
	}

	discussionID := NewID()
	_, err = tx.ExecContext(ctx, `
INSERT INTO public_court_discussions (id, case_id, user_id, body, created_at)
VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
`, discussionID, caseID, userID, body)
	if err != nil {
		return nil, err
	}

	var row PublicCourtDiscussion
	err = tx.GetContext(ctx, &row, `
SELECT d.id, d.case_id, d.user_id,
       COALESCE(u.uid, '') AS user_uid,
       COALESCE(NULLIF(u.display_name, ''), u.username, '') AS user_name,
       COALESCE(u.avatar_url, '') AS user_avatar,
       d.body, d.created_at
FROM public_court_discussions d
LEFT JOIN users u ON u.id = d.user_id
WHERE d.id = $1
`, discussionID)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return &row, nil
}

func (s *PublicCourtStore) CastVote(ctx context.Context, caseID, userID, vote, reason, evidence string) (*PublicCourtVoteResult, error) {
	if caseID == "" || userID == "" {
		return nil, ErrNotFound
	}
	vote = normalizePublicCourtVote(vote)
	if vote == "" {
		return nil, ErrPublicCourtInvalidVote
	}
	reason = strings.TrimSpace(reason)
	evidence = strings.TrimSpace(evidence)
	if evidence == "" {
		return nil, ErrPublicCourtVoteEvidenceRequired
	}

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var item PublicCourtCase
	err = tx.GetContext(ctx, &item, `
SELECT id, report_id, reporter_id, reporter_uid, defendant_id, defendant_uid,
       report_reason, report_evidence, defense_reason, defense_evidence,
       status, verdict, admin_note, ban_hours, reward_processed,
       closed_at, created_at, updated_at
FROM public_court_cases
WHERE id = $1
`, caseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if item.Status != "open" {
		return nil, ErrPublicCourtClosed
	}
	var reputation int
	err = tx.GetContext(ctx, &reputation, `
SELECT reputation_score
FROM users
WHERE id = $1
`, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if reputation <= publicCourtVoteMinReputation {
		return nil, ErrPublicCourtReputationTooLow
	}
	isParty := userID == item.ReporterID || userID == item.DefendantID

	_, err = tx.ExecContext(ctx, `
INSERT INTO public_court_votes (case_id, voter_id, vote, reason, evidence, created_at)
VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
ON CONFLICT(case_id, voter_id)
DO UPDATE SET vote = excluded.vote,
              reason = excluded.reason,
              evidence = excluded.evidence,
              created_at = CURRENT_TIMESTAMP
`, caseID, userID, vote, reason, evidence)
	if err != nil {
		return nil, err
	}

	if !isParty {
		_, err = tx.ExecContext(ctx, `
INSERT INTO public_court_statements (
    case_id, user_id, role, reason, evidence, created_at, updated_at
) VALUES (
    $1, $2, 'jury', $3, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
ON CONFLICT(case_id, user_id)
DO UPDATE SET role = 'jury',
              reason = excluded.reason,
              evidence = excluded.evidence,
              updated_at = CURRENT_TIMESTAMP
`, caseID, userID, reason, evidence)
		if err != nil {
			return nil, err
		}
	}

	result := &PublicCourtVoteResult{
		CaseID:      caseID,
		CaseStatus:  item.Status,
		DefendantID: item.DefendantID,
	}
	err = tx.QueryRowContext(ctx, `
SELECT
    SUM(CASE WHEN vote = 'ban' THEN 1 ELSE 0 END) AS ban_vote_count,
    SUM(CASE WHEN vote = 'keep' THEN 1 ELSE 0 END) AS keep_vote_count,
    COUNT(1) AS total_vote_count
FROM public_court_votes
WHERE case_id = $1
`, caseID).Scan(&result.BanVoteCount, &result.KeepVoteCount, &result.TotalVoteCount)
	if err != nil {
		return nil, err
	}

	if result.TotalVoteCount >= publicCourtVoteLockThreshold {
		juryVerdict := "keep"
		tempBanHours := 0
		if result.BanVoteCount > result.KeepVoteCount {
			juryVerdict = "ban"
			tempBanHours = publicCourtAutoBanHours
		}
		res, updateErr := tx.ExecContext(ctx, `
UPDATE public_court_cases
SET status = 'pending_review',
    verdict = $1,
    ban_hours = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $3 AND status = 'open'
`, juryVerdict, tempBanHours, caseID)
		if updateErr != nil {
			return nil, updateErr
		}
		if rows, _ := res.RowsAffected(); rows > 0 {
			result.CaseStatus = "pending_review"
			result.LockedForReview = true
			result.JuryVerdict = juryVerdict
			result.TemporaryBanHours = tempBanHours
			if result.BanVoteCount != result.KeepVoteCount {
				majorityVote := juryVerdict
				_, err = tx.ExecContext(ctx, `
UPDATE users
SET reputation_score = reputation_score + CASE
        WHEN id IN (SELECT voter_id FROM public_court_votes WHERE case_id = $1 AND vote = $2)
            THEN $3
        ELSE -$4
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT voter_id FROM public_court_votes WHERE case_id = $1)
`, caseID, majorityVote, publicCourtMajorReputationGain, publicCourtMinorReputationLose)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (s *PublicCourtStore) FinalizeCase(ctx context.Context, caseID, verdict, adminNote string, banHours int) (*PublicCourtCase, error) {
	if caseID == "" {
		return nil, ErrNotFound
	}
	verdict = normalizePublicCourtVerdict(verdict)
	if verdict == "" {
		return nil, ErrPublicCourtInvalidVerdict
	}
	if banHours < 0 {
		banHours = 0
	}
	adminNote = strings.TrimSpace(adminNote)

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var item PublicCourtCase
	err = tx.GetContext(ctx, &item, `
SELECT id, report_id, reporter_id, reporter_uid, defendant_id, defendant_uid,
       report_reason, report_evidence, defense_reason, defense_evidence,
       status, verdict, admin_note, ban_hours, reward_processed,
       closed_at, created_at, updated_at
FROM public_court_cases
WHERE id = $1
`, caseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if item.Status == "closed" || item.Status == "withdrawn" {
		return nil, ErrPublicCourtAlreadyClosed
	}

	realBanHours := 0
	if verdict == "ban" {
		realBanHours = banHours
	}
	_, err = tx.ExecContext(ctx, `
UPDATE public_court_cases
SET status = 'closed',
    verdict = $1,
    admin_note = $2,
    ban_hours = $3,
    closed_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $4
`, verdict, adminNote, realBanHours, caseID)
	if err != nil {
		return nil, err
	}

	if item.RewardProcessed == 0 {
		var votes []PublicCourtVote
		err = tx.SelectContext(ctx, &votes, `
SELECT case_id, voter_id, '' AS voter_uid, vote, reason, created_at
FROM public_court_votes
WHERE case_id = $1
`, caseID)
		if err != nil {
			return nil, err
		}
		for _, it := range votes {
			delta := publicCourtCoinDelta(verdict, it.Vote)
			if delta == 0 {
				continue
			}
			if delta > 0 {
				_, err = tx.ExecContext(ctx, `
UPDATE users
SET coin_balance = coin_balance + $1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, delta, it.VoterID)
			} else {
				deduction := -delta
				_, err = tx.ExecContext(ctx, `
UPDATE users
SET coin_balance = CASE WHEN coin_balance >= $1 THEN coin_balance - $1 ELSE 0 END,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, deduction, it.VoterID)
			}
			if err != nil {
				return nil, err
			}
		}
		_, err = tx.ExecContext(ctx, `
UPDATE public_court_cases
SET reward_processed = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1
`, caseID)
		if err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil

	updated, err := s.GetByID(ctx, caseID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *PublicCourtStore) WithdrawCase(ctx context.Context, caseID, reporterID, reason string) (*PublicCourtCase, error) {
	if caseID == "" || reporterID == "" {
		return nil, ErrNotFound
	}
	reason = strings.TrimSpace(reason)

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var item PublicCourtCase
	err = tx.GetContext(ctx, &item, `
SELECT id, report_id, reporter_id, reporter_uid, defendant_id, defendant_uid,
       report_reason, report_evidence, defense_reason, defense_evidence,
       status, verdict, admin_note, ban_hours, reward_processed,
       closed_at, created_at, updated_at
FROM public_court_cases
WHERE id = $1
`, caseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if item.ReporterID != reporterID {
		return nil, ErrPublicCourtForbidden
	}
	if item.Status == "closed" || item.Status == "withdrawn" {
		return nil, ErrPublicCourtClosed
	}
	if item.Status != "open" && item.Status != "pending_review" {
		return nil, ErrPublicCourtClosed
	}

	adminNote := "举报方已撤销举报"
	if reason != "" {
		adminNote = adminNote + "：" + reason
	}

	_, err = tx.ExecContext(ctx, `
UPDATE public_court_cases
SET status = 'withdrawn',
    verdict = 'keep',
    admin_note = $1,
    ban_hours = 0,
    closed_at = CURRENT_TIMESTAMP,
    reward_processed = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, adminNote, caseID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE users
SET reputation_score = reputation_score - $1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, publicCourtWithdrawPenalty, reporterID)
	if err != nil {
		return nil, err
	}

	if item.BanHours > 0 {
		_, _ = tx.ExecContext(ctx, `
DELETE FROM banned_users
WHERE user_id = $1 AND reason = '公开法庭初审临时封禁'
`, item.DefendantID)
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return s.GetByID(ctx, caseID)
}

func normalizePublicCourtStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "open":
		return "open"
	case "pending_review", "pending", "review":
		return "pending_review"
	case "closed":
		return "closed"
	case "withdrawn", "withdraw":
		return "withdrawn"
	case "all":
		return "all"
	default:
		return "open"
	}
}

func normalizePublicCourtVote(vote string) string {
	switch strings.ToLower(strings.TrimSpace(vote)) {
	case "ban", "封禁", "封号":
		return "ban"
	case "keep", "not_ban", "不封", "不封禁", "不封号":
		return "keep"
	default:
		return ""
	}
}

func normalizePublicCourtVerdict(verdict string) string {
	return normalizePublicCourtVote(verdict)
}

func publicCourtCoinDelta(verdict string, vote string) int {
	v := normalizePublicCourtVote(vote)
	if v == "" {
		return 0
	}
	if normalizePublicCourtVerdict(verdict) == v {
		return 10
	}
	return -5
}

func (s *PublicCourtStore) ClearAllCases(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM public_court_cases`)
	if err != nil {
		return 0, err
	}
	rows, _ := res.RowsAffected()
	if rows < 0 {
		rows = 0
	}
	return rows, nil
}
