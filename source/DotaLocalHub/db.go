package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const maxParseRetryAttempts = 10

var (
	ErrDailyMatchLimit  = errors.New("сегодня уже добавлено 3 личных матча; лимит обновится в 03:00 по МСК")
	ErrDailyLookupLimit = errors.New("сегодня уже использовано 20 попыток найти replay по Match ID; лимит обновится в 03:00 по МСК")
)

func openStore(path string, tournament TournamentConfig) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	log.Printf("SQLite: applying migrations")
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	log.Printf("SQLite: seeding %s teams", tournament.Name)
	if err := store.seedTournament(tournament); err != nil {
		db.Close()
		return nil, err
	}
	log.Printf("SQLite: ready")
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS matches (
    match_id INTEGER PRIMARY KEY,
    cluster INTEGER NOT NULL,
    replay_salt INTEGER NOT NULL,
    start_time INTEGER NOT NULL,
    duration INTEGER NOT NULL,
    radiant_win INTEGER NOT NULL,
    game_mode INTEGER NOT NULL,
    lobby_type INTEGER NOT NULL,
    replay_path TEXT NOT NULL,
    parsed_at TEXT NOT NULL,
    parser_version INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    anon_token_hash TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS user_matches (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    match_id INTEGER NOT NULL REFERENCES matches(match_id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT '',
    added_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, match_id)
);

CREATE INDEX IF NOT EXISTS user_matches_user_added_idx
ON user_matches(user_id, added_at DESC);

CREATE TABLE IF NOT EXISTS user_daily_usage (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bucket_date TEXT NOT NULL,
    match_add_count INTEGER NOT NULL DEFAULT 0,
    match_reserved_count INTEGER NOT NULL DEFAULT 0,
    lookup_attempt_count INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, bucket_date)
);

CREATE TABLE IF NOT EXISTS players (
    match_id INTEGER NOT NULL REFERENCES matches(match_id) ON DELETE CASCADE,
    player_index INTEGER NOT NULL,
    name TEXT NOT NULL,
    steam_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL,
    hero_id INTEGER NOT NULL,
    team TEXT NOT NULL,
    team_slot INTEGER NOT NULL,
    kills INTEGER NOT NULL,
    deaths INTEGER NOT NULL,
    assists INTEGER NOT NULL,
    cs INTEGER NOT NULL,
    last_hits INTEGER NOT NULL,
    denies INTEGER NOT NULL,
    gpm REAL NOT NULL,
    total_earned_gold INTEGER NOT NULL,
    madstone INTEGER NOT NULL,
    tower_kills INTEGER NOT NULL,
    wards_placed INTEGER NOT NULL,
    camps_stacked INTEGER NOT NULL,
    runes_grabbed INTEGER NOT NULL,
    watchers INTEGER NOT NULL,
    lotuses INTEGER NOT NULL,
    roshan_kills INTEGER NOT NULL,
    teamfight_participation REAL NOT NULL,
    stuns REAL NOT NULL,
    tormentors INTEGER NOT NULL,
    courier_kills INTEGER NOT NULL,
    first_blood INTEGER NOT NULL,
    smokes INTEGER NOT NULL,
    PRIMARY KEY (match_id, player_index)
);

CREATE INDEX IF NOT EXISTS players_account_idx ON players(account_id);

CREATE TABLE IF NOT EXISTS tournament_teams (
    slug TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    tag TEXT NOT NULL,
    status TEXT NOT NULL,
    slot_order INTEGER NOT NULL,
    opendota_id INTEGER NOT NULL DEFAULT 0,
    history_team_id INTEGER NOT NULL DEFAULT 0,
    logo_url TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS team_rosters (
    team_slug TEXT NOT NULL REFERENCES tournament_teams(slug) ON DELETE CASCADE,
    account_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    persona_name TEXT NOT NULL DEFAULT '',
    avatar_url TEXT NOT NULL DEFAULT '',
    portrait_url TEXT NOT NULL DEFAULT '',
    profile_checked_at INTEGER NOT NULL DEFAULT 0,
    portrait_checked_at INTEGER NOT NULL DEFAULT 0,
    position INTEGER NOT NULL,
    PRIMARY KEY (team_slug, account_id)
);

CREATE TABLE IF NOT EXISTS team_matches (
    team_slug TEXT NOT NULL REFERENCES tournament_teams(slug) ON DELETE CASCADE,
    match_id INTEGER NOT NULL,
    league_id INTEGER NOT NULL DEFAULT 0,
    start_time INTEGER NOT NULL,
    duration INTEGER NOT NULL,
    opponent_team_id INTEGER NOT NULL DEFAULT 0,
    opponent_name TEXT NOT NULL DEFAULT '',
    opponent_logo TEXT NOT NULL DEFAULT '',
    team_won INTEGER NOT NULL DEFAULT 0,
    team_score INTEGER NOT NULL DEFAULT 0,
    opponent_score INTEGER NOT NULL DEFAULT 0,
    league_name TEXT NOT NULL DEFAULT '',
    series_id INTEGER NOT NULL DEFAULT 0,
    series_type INTEGER NOT NULL DEFAULT 0,
    roster_matched INTEGER NOT NULL DEFAULT 0,
    parse_status TEXT NOT NULL DEFAULT 'pending',
    parse_error TEXT NOT NULL DEFAULT '',
    included INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (team_slug, match_id)
);

CREATE INDEX IF NOT EXISTS team_matches_status_idx
ON team_matches(team_slug, roster_matched, parse_status, start_time DESC);

CREATE TABLE IF NOT EXISTS parse_retries (
    user_id TEXT NOT NULL DEFAULT '',
    match_id INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'retrying',
    error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT '',
    first_attempt_at INTEGER NOT NULL,
    last_attempt_at INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, match_id)
);

CREATE INDEX IF NOT EXISTS parse_retries_due_idx
ON parse_retries(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS maintenance_migrations (
    name TEXT PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

UPDATE team_matches
SET parse_status = 'pending',
    parse_error = 'Предыдущий запуск приложения был прерван; матч будет обработан повторно'
WHERE parse_status = 'parsing';

UPDATE team_matches
SET parse_error = 'Dota 2 не сохранила доступный реплей или срок его хранения закончился'
WHERE parse_status = 'unavailable' AND (
    parse_error = '' OR parse_error LIKE '%replay_salt%'
);

UPDATE team_matches
SET parse_status = 'done',
    parse_error = ''
WHERE parse_status <> 'done'
  AND EXISTS (SELECT 1 FROM matches WHERE matches.match_id = team_matches.match_id);
`)
	if err != nil {
		return err
	}
	for _, migration := range []struct {
		table, column, definition string
	}{
		{"matches", "parser_version", "INTEGER NOT NULL DEFAULT 1"},
		{"team_rosters", "persona_name", "TEXT NOT NULL DEFAULT ''"},
		{"team_rosters", "avatar_url", "TEXT NOT NULL DEFAULT ''"},
		{"team_rosters", "portrait_url", "TEXT NOT NULL DEFAULT ''"},
		{"team_rosters", "profile_checked_at", "INTEGER NOT NULL DEFAULT 0"},
		{"team_rosters", "portrait_checked_at", "INTEGER NOT NULL DEFAULT 0"},
		{"team_matches", "included", "INTEGER NOT NULL DEFAULT 1"},
		{"team_matches", "league_id", "INTEGER NOT NULL DEFAULT 0"},
		{"team_matches", "series_id", "INTEGER NOT NULL DEFAULT 0"},
		{"team_matches", "series_type", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.ensureColumn(migration.table, migration.column, migration.definition); err != nil {
			return err
		}
	}
	if err := s.migrateParseRetriesUserScope(); err != nil {
		return err
	}
	if err := s.requeueValveZstdFailures(); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
UPDATE team_rosters SET profile_checked_at = unixepoch()
WHERE profile_checked_at = 0 AND persona_name <> '' AND avatar_url <> '';
UPDATE team_rosters SET portrait_checked_at = unixepoch()
WHERE portrait_checked_at = 0 AND portrait_url <> '';`); err != nil {
		return err
	}
	return s.recalculateStoredGPM()
}

func (s *Store) requeueValveZstdFailures() error {
	const migrationName = "2026-08-01-valve-zstd-requeue-v2"
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var applied int
	err = tx.QueryRow(`SELECT 1 FROM maintenance_migrations WHERE name = ?`, migrationName).Scan(&applied)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	const affectedMatches = `
SELECT DISTINCT match_id
FROM team_matches
WHERE parse_status = 'error'
  AND league_id IN (19917, 20009)
  AND (
      lower(parse_error) LIKE '%bzip2%'
      OR lower(parse_error) LIKE '%bad magic%'
      OR lower(parse_error) LIKE '%http 502%'
  )`
	if _, err := tx.Exec(`DELETE FROM parse_retries WHERE match_id IN (` + affectedMatches + `)`); err != nil {
		return err
	}
	result, err := tx.Exec(`
UPDATE team_matches
SET parse_status = 'pending',
    parse_error = 'Повтор после добавления поддержки Zstandard replay Valve'
WHERE match_id IN (` + affectedMatches + `)`)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO maintenance_migrations (name, applied_at) VALUES (?, ?)`, migrationName, time.Now().Unix()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err == nil && count > 0 {
		log.Printf("SQLite: requeued %d team match rows after Valve Zstandard transition", count)
	}
	return nil
}

func (s *Store) migrateParseRetriesUserScope() error {
	rows, err := s.db.Query(`PRAGMA table_info(parse_retries)`)
	if err != nil {
		return err
	}
	hasUserID := false
	userIDPrimaryKey := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "user_id" {
			hasUserID = true
			userIDPrimaryKey = primaryKey > 0
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if hasUserID && userIDPrimaryKey {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DROP INDEX IF EXISTS parse_retries_due_idx`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE parse_retries RENAME TO parse_retries_old`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE parse_retries (
    user_id TEXT NOT NULL DEFAULT '',
    match_id INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'retrying',
    error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT '',
    first_attempt_at INTEGER NOT NULL,
    last_attempt_at INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, match_id)
)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT OR IGNORE INTO parse_retries (
    user_id, match_id, status, error, attempts, source,
    first_attempt_at, last_attempt_at, next_attempt_at, updated_at
)
SELECT '', match_id, status, error, attempts, source,
       first_attempt_at, last_attempt_at, next_attempt_at, updated_at
FROM parse_retries_old`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE parse_retries_old`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX parse_retries_due_idx ON parse_retries(status, next_attempt_at)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ensureColumn(table, column, definition string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			rows.Close()
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func (s *Store) recalculateStoredGPM() error {
	_, err := s.db.Exec(`
UPDATE players
SET gpm = (
    SELECT players.total_earned_gold * 60.0 / matches.duration
    FROM matches
    WHERE matches.match_id = players.match_id
      AND matches.duration > 0
)
WHERE EXISTS (
    SELECT 1
    FROM matches
    WHERE matches.match_id = players.match_id
      AND matches.duration > 0
);
`)
	return err
}

func (s *Store) EnsureAnonymousUser(ctx context.Context, tokenHash string) (string, bool, error) {
	now := time.Now().Unix()
	var userID string
	err := s.db.QueryRowContext(ctx, `
SELECT id FROM users WHERE anon_token_hash = ?`, tokenHash).Scan(&userID)
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE users SET last_seen_at = ? WHERE id = ?`, now, userID)
		return userID, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}

	for attempt := 0; attempt < 3; attempt++ {
		token, tokenErr := randomToken(18)
		if tokenErr != nil {
			return "", false, tokenErr
		}
		userID = "u_" + token
		_, err = s.db.ExecContext(ctx, `
INSERT INTO users(id, anon_token_hash, created_at, last_seen_at)
VALUES (?, ?, ?, ?)`, userID, tokenHash, now, now)
		if err == nil {
			return userID, false, nil
		}
	}
	return "", false, err
}

func quotaBucketMSK(now time.Time) string {
	msk := time.FixedZone("MSK", 3*60*60)
	local := now.In(msk)
	if local.Hour() < 3 {
		local = local.AddDate(0, 0, -1)
	}
	return local.Format("2006-01-02")
}

func (s *Store) ensureDailyUsageRow(ctx context.Context, tx *sql.Tx, userID, bucket string, now int64) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO user_daily_usage(user_id, bucket_date, created_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(user_id, bucket_date) DO NOTHING`, userID, bucket, now, now)
	return err
}

func (s *Store) ReserveDailyMatchSlot(ctx context.Context, userID string, limit int) error {
	if limit <= 0 {
		return nil
	}
	now := time.Now().Unix()
	bucket := quotaBucketMSK(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.ensureDailyUsageRow(ctx, tx, userID, bucket, now); err != nil {
		return err
	}
	var used, reserved int
	if err := tx.QueryRowContext(ctx, `
SELECT match_add_count, match_reserved_count
FROM user_daily_usage
WHERE user_id = ? AND bucket_date = ?`, userID, bucket).Scan(&used, &reserved); err != nil {
		return err
	}
	if used+reserved >= limit {
		return ErrDailyMatchLimit
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE user_daily_usage
SET match_reserved_count = match_reserved_count + 1,
    updated_at = ?
WHERE user_id = ? AND bucket_date = ?`, now, userID, bucket); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReleaseDailyMatchSlot(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}
	now := time.Now().Unix()
	bucket := quotaBucketMSK(time.Now())
	_, err := s.db.ExecContext(ctx, `
UPDATE user_daily_usage
SET match_reserved_count = max(match_reserved_count - 1, 0),
    updated_at = ?
WHERE user_id = ? AND bucket_date = ?`, now, userID, bucket)
	return err
}

func (s *Store) FinalizeDailyMatchSlot(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}
	now := time.Now().Unix()
	bucket := quotaBucketMSK(time.Now())
	_, err := s.db.ExecContext(ctx, `
UPDATE user_daily_usage
SET match_reserved_count = max(match_reserved_count - 1, 0),
    match_add_count = match_add_count + 1,
    updated_at = ?
WHERE user_id = ? AND bucket_date = ?`, now, userID, bucket)
	return err
}

func (s *Store) ConsumeDailyLookupAttempt(ctx context.Context, userID string, limit int) error {
	if limit <= 0 {
		return nil
	}
	now := time.Now().Unix()
	bucket := quotaBucketMSK(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.ensureDailyUsageRow(ctx, tx, userID, bucket, now); err != nil {
		return err
	}
	var attempts int
	if err := tx.QueryRowContext(ctx, `
SELECT lookup_attempt_count
FROM user_daily_usage
WHERE user_id = ? AND bucket_date = ?`, userID, bucket).Scan(&attempts); err != nil {
		return err
	}
	if attempts >= limit {
		return ErrDailyLookupLimit
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE user_daily_usage
SET lookup_attempt_count = lookup_attempt_count + 1,
    updated_at = ?
WHERE user_id = ? AND bucket_date = ?`, now, userID, bucket); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UserHasMatch(ctx context.Context, userID string, matchID uint64) (bool, error) {
	var value int
	err := s.db.QueryRowContext(ctx, `
SELECT 1 FROM user_matches WHERE user_id = ? AND match_id = ?`, userID, matchID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) LinkUserMatch(ctx context.Context, userID string, matchID uint64, source string) error {
	if userID == "" || matchID == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO user_matches(user_id, match_id, source, added_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(user_id, match_id) DO UPDATE SET
    source = CASE WHEN excluded.source <> '' THEN excluded.source ELSE user_matches.source END`,
		userID, matchID, source, time.Now().Unix())
	return err
}

func (s *Store) UserCanAccessMatch(ctx context.Context, userID string, matchID uint64) (bool, error) {
	var value int
	err := s.db.QueryRowContext(ctx, `
SELECT 1
WHERE EXISTS (SELECT 1 FROM user_matches WHERE user_id = ? AND match_id = ?)
   OR EXISTS (SELECT 1 FROM team_matches WHERE match_id = ?)`,
		userID, matchID, matchID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) seedTournament(tournament TournamentConfig) error {
	if err := validateTournamentConfig(tournament); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	activeSlugs := make([]any, 0, len(tournament.Teams))
	activePlaceholders := make([]string, 0, len(tournament.Teams))
	for _, team := range tournament.Teams {
		activeSlugs = append(activeSlugs, team.Slug)
		activePlaceholders = append(activePlaceholders, "?")

		_, err := tx.Exec(`
INSERT INTO tournament_teams (
    slug, name, tag, status, slot_order, opendota_id, history_team_id, logo_url
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(slug) DO UPDATE SET
    name = excluded.name,
    tag = excluded.tag,
    status = excluded.status,
    slot_order = excluded.slot_order,
    opendota_id = excluded.opendota_id,
    history_team_id = excluded.history_team_id,
    logo_url = excluded.logo_url`,
			team.Slug, team.Name, team.Tag, team.Status, team.SlotOrder,
			team.OpenDotaID, team.HistoryTeamID, team.LogoURL)
		if err != nil {
			return err
		}

		activeRoster := make([]any, 0, len(team.Roster)+1)
		activeRosterPlaceholders := make([]string, 0, len(team.Roster))
		for _, player := range team.Roster {
			activeRoster = append(activeRoster, player.AccountID)
			activeRosterPlaceholders = append(activeRosterPlaceholders, "?")
			if _, err := tx.Exec(`
INSERT INTO team_rosters(team_slug, account_id, name, persona_name, avatar_url, portrait_url, position)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(team_slug, account_id) DO UPDATE SET
    name = excluded.name,
    persona_name = CASE WHEN excluded.persona_name <> '' THEN excluded.persona_name ELSE team_rosters.persona_name END,
    avatar_url = CASE WHEN excluded.avatar_url <> '' THEN excluded.avatar_url ELSE team_rosters.avatar_url END,
    portrait_url = CASE WHEN excluded.portrait_url <> '' THEN excluded.portrait_url ELSE team_rosters.portrait_url END,
    position = excluded.position`,
				team.Slug, player.AccountID, player.Name, player.PersonaName, player.AvatarURL, player.PortraitURL, player.Position); err != nil {
				return err
			}
		}
		if len(activeRosterPlaceholders) == 0 {
			if _, err := tx.Exec(`DELETE FROM team_rosters WHERE team_slug = ?`, team.Slug); err != nil {
				return err
			}
		} else {
			activeRoster = append([]any{team.Slug}, activeRoster...)
			query := fmt.Sprintf(`DELETE FROM team_rosters WHERE team_slug = ? AND account_id NOT IN (%s)`, strings.Join(activeRosterPlaceholders, ","))
			if _, err := tx.Exec(query, activeRoster...); err != nil {
				return err
			}
		}
	}

	if len(activeSlugs) > 0 {
		query := fmt.Sprintf(`DELETE FROM tournament_teams WHERE status = 'tbd' AND slug NOT IN (%s)`, strings.Join(activePlaceholders, ","))
		if _, err := tx.Exec(query, activeSlugs...); err != nil {
			return err
		}

		replaceableSlotsMap := replaceableTBDSlotSet(tournament)
		replaceableSlots := make([]any, 0, len(replaceableSlotsMap)+len(activeSlugs))
		replaceablePlaceholders := make([]string, 0, len(replaceableSlotsMap))
		for slot := range replaceableSlotsMap {
			replaceableSlots = append(replaceableSlots, slot)
			replaceablePlaceholders = append(replaceablePlaceholders, "?")
		}
		if len(replaceableSlots) > 0 {
			query = fmt.Sprintf(
				`DELETE FROM tournament_teams WHERE slot_order IN (%s) AND slug NOT IN (%s)`,
				strings.Join(replaceablePlaceholders, ","),
				strings.Join(activePlaceholders, ","),
			)
			args := append(replaceableSlots, activeSlugs...)
			if _, err := tx.Exec(query, args...); err != nil {
				return err
			}
		}
	}

	if _, err := tx.Exec(`
UPDATE team_rosters
SET portrait_url = ''
WHERE lower(portrait_url) LIKE '%itemicon%'
   OR lower(portrait_url) LIKE '%gameasset%'
   OR lower(portrait_url) LIKE '%minimap_icon%'
   OR lower(portrait_url) LIKE '%ability%'`); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) MatchExists(ctx context.Context, matchID uint64) (bool, error) {
	var value int
	err := s.db.QueryRowContext(ctx, `
SELECT 1 FROM matches WHERE match_id = ? AND parser_version >= ?`,
		matchID, currentParserVersion).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) SaveMatch(ctx context.Context, match MatchRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
INSERT INTO matches (
    match_id, cluster, replay_salt, start_time, duration, radiant_win,
    game_mode, lobby_type, replay_path, parsed_at, parser_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(match_id) DO UPDATE SET
    cluster = excluded.cluster,
    replay_salt = excluded.replay_salt,
    start_time = excluded.start_time,
    duration = excluded.duration,
    radiant_win = excluded.radiant_win,
    game_mode = excluded.game_mode,
    lobby_type = excluded.lobby_type,
    replay_path = excluded.replay_path,
    parsed_at = excluded.parsed_at,
    parser_version = excluded.parser_version`,
		match.MatchID, match.Cluster, match.ReplaySalt, match.StartTime,
		match.Duration, boolToInt(match.RadiantWin), match.GameMode,
		match.LobbyType, match.ReplayPath,
		match.ParsedAt.UTC().Format(time.RFC3339Nano), currentParserVersion)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM players WHERE match_id = ?`, match.MatchID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM parse_retries WHERE match_id = ?`, match.MatchID); err != nil {
		return err
	}

	statement := `
INSERT INTO players (
    match_id, player_index, name, steam_id, account_id, hero_id, team, team_slot,
    kills, deaths, assists, cs, last_hits, denies, gpm, total_earned_gold,
    madstone, tower_kills, wards_placed, camps_stacked, runes_grabbed, watchers,
    lotuses, roshan_kills, teamfight_participation, stuns, tormentors,
    courier_kills, first_blood, smokes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	for _, player := range match.Players {
		_, err := tx.ExecContext(ctx, statement,
			match.MatchID, player.PlayerIndex, player.Name, player.SteamID,
			player.AccountID, player.HeroID, player.Team, player.TeamSlot,
			player.Kills, player.Deaths, player.Assists, player.CS,
			player.LastHits, player.Denies, player.GPM, player.TotalEarnedGold,
			player.Madstone, player.TowerKills, player.WardsPlaced,
			player.CampsStacked, player.RunesGrabbed, player.Watchers,
			player.Lotuses, player.RoshanKills, player.TeamfightParticipation,
			player.Stuns, player.Tormentors, player.CourierKills,
			player.FirstBlood, player.Smokes)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpsertParseRetry(ctx context.Context, userID string, matchID uint64, source string, parseErr error) error {
	now := time.Now().Unix()
	message := ""
	if parseErr != nil {
		message = parseErr.Error()
	}

	var firstAttempt int64
	var attempts int
	err := s.db.QueryRowContext(ctx, `
SELECT first_attempt_at, attempts
FROM parse_retries
WHERE user_id = ? AND match_id = ?`, userID, matchID).Scan(&firstAttempt, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		firstAttempt = now
		attempts = 0
	} else if err != nil {
		return err
	}

	attempts++
	status := "retrying"
	nextAttempt := nextParseRetryAt(attempts, firstAttempt, now)
	if nextAttempt == 0 {
		status = "stopped"
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO parse_retries (
    user_id, match_id, status, error, attempts, source,
    first_attempt_at, last_attempt_at, next_attempt_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, match_id) DO UPDATE SET
    status = excluded.status,
    error = excluded.error,
    attempts = excluded.attempts,
    source = excluded.source,
    last_attempt_at = excluded.last_attempt_at,
    next_attempt_at = excluded.next_attempt_at,
    updated_at = excluded.updated_at`,
		userID, matchID, status, message, attempts, source, firstAttempt, now, nextAttempt, now)
	return err
}

func nextParseRetryAt(attempts int, firstAttempt, now int64) int64 {
	if attempts >= maxParseRetryAttempts {
		return 0
	}
	age := time.Duration(now-firstAttempt) * time.Second
	if age >= 24*time.Hour {
		return 0
	}
	delays := [...]time.Duration{
		5 * time.Minute,
		10 * time.Minute,
		20 * time.Minute,
		40 * time.Minute,
		time.Hour,
		2 * time.Hour,
		4 * time.Hour,
		4 * time.Hour,
		4 * time.Hour,
	}
	index := attempts - 1
	if index < 0 {
		index = 0
	}
	if index >= len(delays) {
		index = len(delays) - 1
	}
	return now + int64(delays[index].Seconds())
}

func (s *Store) ListParseRetries(ctx context.Context, userID string) ([]ParseRetryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT user_id, match_id, status, error, attempts, source,
       first_attempt_at, last_attempt_at, next_attempt_at, updated_at
FROM parse_retries
WHERE user_id = ?
ORDER BY CASE status WHEN 'retrying' THEN 0 ELSE 1 END,
         updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]ParseRetryRecord, 0)
	for rows.Next() {
		var record ParseRetryRecord
		if err := rows.Scan(
			&record.UserID, &record.MatchID, &record.Status, &record.Error, &record.Attempts, &record.Source,
			&record.FirstAttemptAt, &record.LastAttemptAt, &record.NextAttemptAt, &record.UpdatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) DueParseRetries(ctx context.Context, limit int) ([]ParseRetryRecord, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT user_id, match_id, status, error, attempts, source,
       first_attempt_at, last_attempt_at, next_attempt_at, updated_at
FROM parse_retries
WHERE status = 'retrying' AND next_attempt_at > 0 AND next_attempt_at <= ?
ORDER BY next_attempt_at ASC
LIMIT ?`, time.Now().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]ParseRetryRecord, 0)
	for rows.Next() {
		var record ParseRetryRecord
		if err := rows.Scan(
			&record.UserID, &record.MatchID, &record.Status, &record.Error, &record.Attempts, &record.Source,
			&record.FirstAttemptAt, &record.LastAttemptAt, &record.NextAttemptAt, &record.UpdatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) DeleteParseRetry(ctx context.Context, userID string, matchID uint64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM parse_retries WHERE user_id = ? AND match_id = ?`, userID, matchID)
	return err
}

func (s *Store) StopParseRetry(ctx context.Context, userID string, matchID uint64) (bool, error) {
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, `
UPDATE parse_retries
SET status = 'stopped',
    next_attempt_at = 0,
    updated_at = ?
WHERE user_id = ? AND match_id = ?`, now, userID, matchID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *Store) ClearReplayPath(ctx context.Context, matchID uint64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE matches SET replay_path = '' WHERE match_id = ?`, matchID)
	return err
}

func (s *Store) ClearAllReplayPaths(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE matches SET replay_path = ''`)
	return err
}

func (s *Store) ListMatches(ctx context.Context, userID string) ([]MatchRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT m.match_id, m.cluster, m.replay_salt, m.start_time, m.duration,
       m.radiant_win, m.game_mode, m.lobby_type, m.replay_path, m.parsed_at,
       COALESCE(SUM(CASE WHEN p.team = 'radiant' THEN p.kills ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN p.team = 'dire' THEN p.kills ELSE 0 END), 0)
FROM user_matches um
JOIN matches m ON m.match_id = um.match_id
LEFT JOIN players p ON p.match_id = m.match_id
WHERE um.user_id = ?
GROUP BY m.match_id
ORDER BY um.added_at DESC, m.parsed_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	matches := make([]MatchRecord, 0)
	for rows.Next() {
		var match MatchRecord
		var radiantWin int
		var parsedAt string
		if err := rows.Scan(
			&match.MatchID, &match.Cluster, &match.ReplaySalt, &match.StartTime,
			&match.Duration, &radiantWin, &match.GameMode, &match.LobbyType,
			&match.ReplayPath, &parsedAt, &match.RadiantKills, &match.DireKills,
		); err != nil {
			return nil, err
		}
		match.RadiantWin = radiantWin != 0
		match.ParsedAt, _ = time.Parse(time.RFC3339Nano, parsedAt)
		matches = append(matches, match)
	}
	return matches, rows.Err()
}

func (s *Store) GetMatch(ctx context.Context, matchID uint64) (MatchRecord, error) {
	var match MatchRecord
	var radiantWin int
	var parsedAt string
	err := s.db.QueryRowContext(ctx, `
SELECT match_id, cluster, replay_salt, start_time, duration, radiant_win,
       game_mode, lobby_type, replay_path, parsed_at
FROM matches WHERE match_id = ?`, matchID).Scan(
		&match.MatchID, &match.Cluster, &match.ReplaySalt, &match.StartTime,
		&match.Duration, &radiantWin, &match.GameMode, &match.LobbyType,
		&match.ReplayPath, &parsedAt,
	)
	if err != nil {
		return match, err
	}
	match.RadiantWin = radiantWin != 0
	match.ParsedAt, _ = time.Parse(time.RFC3339Nano, parsedAt)

	rows, err := s.db.QueryContext(ctx, playerSelect+`
FROM players WHERE match_id = ?
ORDER BY CASE team WHEN 'radiant' THEN 0 ELSE 1 END, team_slot`, matchID)
	if err != nil {
		return match, err
	}
	defer rows.Close()

	for rows.Next() {
		player, err := scanPlayer(rows)
		if err != nil {
			return match, err
		}
		match.Players = append(match.Players, player)
		if player.Team == "radiant" {
			match.RadiantKills += player.Kills
		} else {
			match.DireKills += player.Kills
		}
	}
	return match, rows.Err()
}

func (s *Store) ListTeams(ctx context.Context) ([]TournamentTeam, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.slug, t.name, t.tag, t.status, t.slot_order, t.opendota_id,
       t.history_team_id, t.logo_url,
       COUNT(tm.match_id),
       COALESCE(SUM(CASE WHEN tm.parse_status = 'done' THEN 1 ELSE 0 END), 0)
FROM tournament_teams t
LEFT JOIN team_matches tm ON tm.team_slug = t.slug AND tm.roster_matched = 1
GROUP BY t.slug
ORDER BY t.slot_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	teams := make([]TournamentTeam, 0, 16)
	for rows.Next() {
		var team TournamentTeam
		if err := rows.Scan(
			&team.Slug, &team.Name, &team.Tag, &team.Status, &team.SlotOrder,
			&team.OpenDotaID, &team.HistoryTeamID, &team.LogoURL,
			&team.MatchCount, &team.ParsedCount,
		); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (s *Store) GetTeam(ctx context.Context, slug string) (TournamentTeam, error) {
	var team TournamentTeam
	err := s.db.QueryRowContext(ctx, `
SELECT slug, name, tag, status, slot_order, opendota_id, history_team_id, logo_url
FROM tournament_teams WHERE slug = ?`, slug).Scan(
		&team.Slug, &team.Name, &team.Tag, &team.Status, &team.SlotOrder,
		&team.OpenDotaID, &team.HistoryTeamID, &team.LogoURL,
	)
	if err != nil {
		return team, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT account_id, name, persona_name, avatar_url, portrait_url, position,
       profile_checked_at, portrait_checked_at
FROM team_rosters WHERE team_slug = ? ORDER BY position`, slug)
	if err != nil {
		return team, err
	}
	for rows.Next() {
		var player TeamPlayer
		if err := rows.Scan(&player.AccountID, &player.Name, &player.PersonaName, &player.AvatarURL, &player.PortraitURL, &player.Position,
			&player.ProfileCheckedAt, &player.PortraitCheckedAt); err != nil {
			rows.Close()
			return team, err
		}
		team.Roster = append(team.Roster, player)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return team, err
	}
	if err := rows.Close(); err != nil {
		return team, err
	}

	for index := range team.Roster {
		player := &team.Roster[index]
		stats, err := s.PlayerFantasyStats(ctx, slug, player.AccountID)
		if err != nil {
			return team, err
		}
		player.Stats = &stats
	}
	return team, nil
}

func (s *Store) TeamRosterAccountIDs(ctx context.Context, slug string) ([]uint32, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT account_id FROM team_rosters WHERE team_slug = ? ORDER BY position`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uint32
	for rows.Next() {
		var id uint32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) SyncableTeamSlugs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.slug
FROM tournament_teams t
WHERE t.status <> 'tbd'
  AND (SELECT COUNT(*) FROM team_rosters tr WHERE tr.team_slug = t.slug) = 5
ORDER BY t.slot_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		slugs = append(slugs, slug)
	}
	return slugs, rows.Err()
}

func (s *Store) UpsertTeamMatch(ctx context.Context, match TeamMatchRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO team_matches (
    team_slug, match_id, league_id, start_time, duration, opponent_team_id, opponent_name,
    opponent_logo, team_won, team_score, opponent_score, league_name, series_id, series_type,
    roster_matched, parse_status, parse_error, included
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(team_slug, match_id) DO UPDATE SET
    league_id = CASE
        WHEN excluded.league_id <> 0 THEN excluded.league_id
        ELSE team_matches.league_id
    END,
    start_time = excluded.start_time,
    duration = excluded.duration,
    opponent_team_id = excluded.opponent_team_id,
    opponent_name = excluded.opponent_name,
    opponent_logo = excluded.opponent_logo,
    team_won = excluded.team_won,
    team_score = excluded.team_score,
    opponent_score = excluded.opponent_score,
    league_name = CASE
        WHEN excluded.league_name <> '' THEN excluded.league_name
        ELSE team_matches.league_name
    END,
    series_id = excluded.series_id,
    series_type = excluded.series_type,
    roster_matched = excluded.roster_matched,
    parse_status = CASE
        WHEN team_matches.parse_status = 'done' THEN 'done'
        ELSE excluded.parse_status
    END,
    parse_error = CASE
        WHEN team_matches.parse_status = 'done' THEN ''
        ELSE excluded.parse_error
    END`,
		match.TeamSlug, match.MatchID, match.LeagueID, match.StartTime, match.Duration,
		match.OpponentTeamID, match.OpponentName, match.OpponentLogo,
		boolToInt(match.TeamWon), match.TeamScore, match.OpponentScore,
		match.LeagueName, match.SeriesID, match.SeriesType,
		boolToInt(match.RosterMatched), match.ParseStatus,
		match.ParseError, boolToInt(match.Included))
	return err
}

func (s *Store) ClearTeamMatches(ctx context.Context, slug string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM team_matches WHERE team_slug = ?`, slug)
	return err
}

func (s *Store) SetTeamMatchStatus(ctx context.Context, slug string, matchID uint64, status, message string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE team_matches SET parse_status = ?, parse_error = ?
WHERE team_slug = ? AND match_id = ?`, status, message, slug, matchID)
	return err
}

func (s *Store) SetAllTeamMatchStatuses(ctx context.Context, matchID uint64, status, message string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE team_matches SET parse_status = ?, parse_error = ? WHERE match_id = ?`, status, message, matchID)
	return err
}

func (s *Store) TeamMatchStatus(ctx context.Context, slug string, matchID uint64) (string, bool, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `
SELECT parse_status FROM team_matches WHERE team_slug = ? AND match_id = ?`, slug, matchID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return status, true, nil
}

func (s *Store) ListTeamMatches(ctx context.Context, slug string) ([]TeamMatchRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT team_slug, match_id, start_time, duration, opponent_team_id,
       opponent_name, opponent_logo, team_won, team_score, opponent_score,
       league_id, league_name, series_id, series_type, roster_matched, parse_status, parse_error, included
FROM team_matches
WHERE team_slug = ? AND roster_matched = 1
ORDER BY start_time DESC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []TeamMatchRecord
	for rows.Next() {
		var match TeamMatchRecord
		var teamWon, rosterMatched, included int
		if err := rows.Scan(
			&match.TeamSlug, &match.MatchID, &match.StartTime, &match.Duration,
			&match.OpponentTeamID, &match.OpponentName, &match.OpponentLogo,
			&teamWon, &match.TeamScore, &match.OpponentScore, &match.LeagueID, &match.LeagueName,
			&match.SeriesID, &match.SeriesType,
			&rosterMatched, &match.ParseStatus, &match.ParseError, &included,
		); err != nil {
			return nil, err
		}
		match.TeamWon = teamWon != 0
		match.RosterMatched = rosterMatched != 0
		match.Included = included != 0
		matches = append(matches, match)
	}
	return matches, rows.Err()
}

func (s *Store) PlayerFantasyStats(ctx context.Context, slug string, accountID uint32) (PlayerFantasyStats, error) {
	var avg fantasyAverages
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*),
       COALESCE(AVG(p.kills), 0),
       COALESCE(AVG(p.deaths), 0),
       COALESCE(AVG(p.cs), 0),
       COALESCE(AVG(p.gpm), 0),
       COALESCE(AVG(p.madstone), 0),
       COALESCE(AVG(p.tower_kills), 0),
       COALESCE(AVG(p.wards_placed), 0),
       COALESCE(AVG(p.camps_stacked), 0),
       COALESCE(AVG(p.runes_grabbed), 0),
       COALESCE(AVG(p.watchers), 0),
       COALESCE(AVG(p.lotuses), 0),
       COALESCE(AVG(p.roshan_kills), 0),
       COALESCE(AVG(p.teamfight_participation), 0),
       COALESCE(AVG(p.stuns), 0),
       COALESCE(AVG(p.tormentors), 0),
       COALESCE(AVG(p.courier_kills), 0),
       COALESCE(AVG(p.first_blood), 0),
       COALESCE(AVG(p.smokes), 0)
FROM players p
JOIN team_matches tm ON tm.match_id = p.match_id
WHERE tm.team_slug = ?
  AND tm.roster_matched = 1
  AND tm.parse_status = 'done'
  AND tm.included = 1
  AND p.account_id = ?`, slug, accountID).Scan(
		&avg.Matches, &avg.Kills, &avg.Deaths, &avg.CS, &avg.GPM,
		&avg.Madstone, &avg.TowerKills, &avg.WardsPlaced, &avg.CampsStacked,
		&avg.RunesGrabbed, &avg.Watchers, &avg.Lotuses, &avg.RoshanKills,
		&avg.TeamfightParticipation, &avg.Stuns, &avg.Tormentors,
		&avg.CourierKills, &avg.FirstBlood, &avg.Smokes,
	)
	if err != nil {
		return PlayerFantasyStats{}, err
	}
	stats := calculateFantasyStats(avg)
	records, err := s.playerFantasyRecords(ctx, slug, accountID)
	if err != nil {
		return PlayerFantasyStats{}, err
	}
	stats.BestMatch = records.BestMatch
	stats.BestSeries = records.BestSeries
	return stats, nil
}

const playerSelect = `
SELECT player_index, name, steam_id, account_id, hero_id, team, team_slot,
       kills, deaths, assists, cs, last_hits, denies, gpm, total_earned_gold,
       madstone, tower_kills, wards_placed, camps_stacked, runes_grabbed,
       watchers, lotuses, roshan_kills, teamfight_participation, stuns,
       tormentors, courier_kills, first_blood, smokes,
       COALESCE((SELECT tr.name FROM team_rosters tr WHERE tr.account_id = players.account_id LIMIT 1), ''),
       COALESCE((SELECT tr.avatar_url FROM team_rosters tr WHERE tr.account_id = players.account_id LIMIT 1), '') `

type scanner interface {
	Scan(dest ...any) error
}

func scanPlayer(row scanner) (PlayerRecord, error) {
	var player PlayerRecord
	err := row.Scan(
		&player.PlayerIndex, &player.Name, &player.SteamID, &player.AccountID,
		&player.HeroID, &player.Team, &player.TeamSlot, &player.Kills,
		&player.Deaths, &player.Assists, &player.CS, &player.LastHits,
		&player.Denies, &player.GPM, &player.TotalEarnedGold, &player.Madstone,
		&player.TowerKills, &player.WardsPlaced, &player.CampsStacked,
		&player.RunesGrabbed, &player.Watchers, &player.Lotuses,
		&player.RoshanKills, &player.TeamfightParticipation, &player.Stuns,
		&player.Tormentors, &player.CourierKills, &player.FirstBlood,
		&player.Smokes, &player.ProName, &player.AvatarURL,
	)
	return player, err
}

func (s *Store) UpdateTeamPlayerProfile(ctx context.Context, slug string, profile PlayerProfile) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE team_rosters
SET persona_name = CASE WHEN ? <> '' THEN ? ELSE persona_name END,
    avatar_url = CASE WHEN ? <> '' THEN ? ELSE avatar_url END,
    profile_checked_at = unixepoch()
WHERE team_slug = ? AND account_id = ?`,
		profile.PersonaName, profile.PersonaName, profile.AvatarURL, profile.AvatarURL,
		slug, profile.AccountID)
	return err
}

func (s *Store) UpdateTeamPlayerPortrait(ctx context.Context, slug string, accountID uint32, portraitURL string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE team_rosters SET portrait_url = CASE WHEN ? <> '' THEN ? ELSE portrait_url END,
    portrait_checked_at = unixepoch()
WHERE team_slug = ? AND account_id = ?`,
		portraitURL, portraitURL, slug, accountID)
	return err
}

func (s *Store) UpdatePlayerName(ctx context.Context, accountID uint32, name string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE players SET name = ? WHERE account_id = ?`, name, accountID)
	return err
}

func (s *Store) SetMatchesIncluded(ctx context.Context, slug string, selections []MatchSelection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, selection := range selections {
		if _, err := tx.ExecContext(ctx, `
UPDATE team_matches SET included = ? WHERE team_slug = ? AND match_id = ?`,
			boolToInt(selection.Included), slug, selection.MatchID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func normalizedLeagueSet(leagues []string) map[string]bool {
	result := make(map[string]bool, len(leagues))
	for _, league := range leagues {
		name := strings.TrimSpace(league)
		if name != "" {
			result[name] = true
		}
	}
	return result
}

func (s *Store) applyFilteredSelectionTx(ctx context.Context, tx *sql.Tx, slug string, leagues map[string]bool, limit int, all bool) error {
	rows, err := tx.QueryContext(ctx, `
SELECT match_id, league_name
FROM team_matches
WHERE team_slug = ? AND roster_matched = 1 AND parse_status = 'done'
ORDER BY start_time DESC`, slug)
	if err != nil {
		return err
	}
	var selected []uint64
	for rows.Next() {
		var matchID uint64
		var league string
		if err := rows.Scan(&matchID, &league); err != nil {
			rows.Close()
			return err
		}
		if leagues[league] && (all || limit <= 0 || len(selected) < limit) {
			selected = append(selected, matchID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_matches SET included = 0 WHERE team_slug = ?`, slug); err != nil {
		return err
	}
	for _, matchID := range selected {
		if _, err := tx.ExecContext(ctx, `
UPDATE team_matches SET included = 1 WHERE team_slug = ? AND match_id = ?`, slug, matchID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ApplyFilteredSelection(ctx context.Context, slug string, leagueNames []string, limit int, all bool) error {
	leagues := normalizedLeagueSet(leagueNames)
	if len(leagues) == 0 {
		return errors.New("выбери хотя бы один турнир")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.applyFilteredSelectionTx(ctx, tx, slug, leagues, limit, all); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ApplyGlobalFilteredSelection(ctx context.Context, leagueNames []string, limit int, all bool) error {
	leagues := normalizedLeagueSet(leagueNames)
	if len(leagues) == 0 {
		return errors.New("выбери хотя бы один турнир")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT slug FROM tournament_teams WHERE status <> 'tbd' ORDER BY slot_order`)
	if err != nil {
		return err
	}
	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			rows.Close()
			return err
		}
		slugs = append(slugs, slug)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, slug := range slugs {
		if err := s.applyFilteredSelectionTx(ctx, tx, slug, leagues, limit, all); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GlobalSelectionOverview(ctx context.Context) (SelectionOverview, error) {
	overview := SelectionOverview{Leagues: make([]SelectionLeague, 0)}
	rows, err := s.db.QueryContext(ctx, `
SELECT league_name, COUNT(*), SUM(CASE WHEN included = 1 THEN 1 ELSE 0 END)
FROM team_matches
WHERE roster_matched = 1 AND parse_status = 'done'
GROUP BY league_name
ORDER BY MIN(start_time) ASC`)
	if err != nil {
		return overview, err
	}
	for rows.Next() {
		var league SelectionLeague
		if err := rows.Scan(&league.Name, &league.MatchCount, &league.IncludedCount); err != nil {
			rows.Close()
			return overview, err
		}
		overview.Leagues = append(overview.Leagues, league)
		overview.AllMatches += league.MatchCount
		overview.SelectedMatches += league.IncludedCount
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return overview, err
	}
	if err := rows.Close(); err != nil {
		return overview, err
	}
	err = s.db.QueryRowContext(ctx, `
SELECT COALESCE(MAX(total_count), 0), COALESCE(MAX(selected_count), 0)
FROM (
  SELECT team_slug,
         COUNT(*) AS total_count,
         SUM(CASE WHEN included = 1 THEN 1 ELSE 0 END) AS selected_count
  FROM team_matches
  WHERE roster_matched = 1 AND parse_status = 'done'
  GROUP BY team_slug
)`).Scan(&overview.MaxMatches, &overview.SelectedPerTeam)
	if err != nil {
		return overview, err
	}
	overview.AllSelected = overview.AllMatches > 0 && overview.SelectedMatches == overview.AllMatches
	return overview, nil
}

func (s *Store) TeamMatchCount(ctx context.Context, slug string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM team_matches WHERE team_slug = ? AND roster_matched = 1`, slug).Scan(&count)
	return count, err
}

func (s *Store) TeamNewestMatchTime(ctx context.Context, slug string) (int64, error) {
	var value int64
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(MAX(start_time), 0) FROM team_matches WHERE team_slug = ?`, slug).Scan(&value)
	return value, err
}

func (s *Store) TeamMatchesMissingSeries(ctx context.Context, slug string) ([]uint64, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT match_id FROM team_matches
WHERE team_slug = ? AND series_id = 0 AND parse_status = 'done'
ORDER BY start_time DESC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uint64
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) SetTeamMatchSeries(ctx context.Context, slug string, matchID, seriesID uint64, seriesType int) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE team_matches SET series_id = ?, series_type = ?
WHERE team_slug = ? AND match_id = ?`, seriesID, seriesType, slug, matchID)
	return err
}

func (s *Store) SetMatchIncluded(ctx context.Context, slug string, matchID uint64, included bool) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE team_matches SET included = ? WHERE team_slug = ? AND match_id = ?`,
		boolToInt(included), slug, matchID)
	return err
}

func (s *Store) SetLeagueIncluded(ctx context.Context, slug, league string, included bool) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE team_matches SET included = ?
WHERE team_slug = ? AND league_name = ? AND parse_status = 'done'`,
		boolToInt(included), slug, league)
	return err
}

func (s *Store) ApplySelectionMode(ctx context.Context, slug, mode string) error {
	switch mode {
	case "all":
		_, err := s.db.ExecContext(ctx, `UPDATE team_matches SET included = 1 WHERE team_slug = ? AND parse_status = 'done'`, slug)
		return err
	case "none":
		_, err := s.db.ExecContext(ctx, `UPDATE team_matches SET included = 0 WHERE team_slug = ?`, slug)
		return err
	case "last20":
		_, err := s.db.ExecContext(ctx, `
UPDATE team_matches SET included = CASE WHEN match_id IN (
    SELECT match_id FROM team_matches
    WHERE team_slug = ? AND parse_status = 'done'
    ORDER BY start_time DESC LIMIT 20
) THEN 1 ELSE 0 END
WHERE team_slug = ?`, slug, slug)
		return err
	case "ti":
		_, err := s.db.ExecContext(ctx, `
UPDATE team_matches SET included = CASE
    WHEN parse_status = 'done' AND LOWER(league_name) LIKE '%international%' THEN 1
    ELSE 0 END
WHERE team_slug = ?`, slug)
		return err
	default:
		return fmt.Errorf("неизвестный режим выбора: %s", mode)
	}
}

func (s *Store) playerFantasyRecords(ctx context.Context, slug string, accountID uint32) (PlayerFantasyStats, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT tm.match_id, tm.start_time, tm.series_id,
       p.kills, p.deaths, p.cs, p.gpm, p.madstone, p.tower_kills,
       p.wards_placed, p.camps_stacked, p.runes_grabbed, p.watchers,
       p.lotuses, p.roshan_kills, p.teamfight_participation, p.stuns,
       p.tormentors, p.courier_kills, p.first_blood, p.smokes
FROM team_matches tm
JOIN players p ON p.match_id = tm.match_id
WHERE tm.team_slug = ? AND tm.roster_matched = 1
  AND tm.parse_status = 'done' AND tm.included = 1 AND p.account_id = ?
ORDER BY tm.start_time ASC`, slug, accountID)
	if err != nil {
		return PlayerFantasyStats{}, err
	}
	defer rows.Close()

	type gameScore struct {
		id       uint64
		seriesID uint64
		points   float64
	}
	var games []gameScore
	for rows.Next() {
		var id uint64
		var started int64
		var seriesID uint64
		var avg fantasyAverages
		avg.Matches = 1
		if err := rows.Scan(
			&id, &started, &seriesID, &avg.Kills, &avg.Deaths, &avg.CS, &avg.GPM,
			&avg.Madstone, &avg.TowerKills, &avg.WardsPlaced, &avg.CampsStacked,
			&avg.RunesGrabbed, &avg.Watchers, &avg.Lotuses, &avg.RoshanKills,
			&avg.TeamfightParticipation, &avg.Stuns, &avg.Tormentors,
			&avg.CourierKills, &avg.FirstBlood, &avg.Smokes,
		); err != nil {
			return PlayerFantasyStats{}, err
		}
		if seriesID == 0 {
			seriesID = id
		}
		games = append(games, gameScore{id: id, seriesID: seriesID, points: calculateFantasyStats(avg).TotalPoints})
	}
	if err := rows.Err(); err != nil {
		return PlayerFantasyStats{}, err
	}

	var result PlayerFantasyStats
	for _, game := range games {
		if len(result.BestMatch.MatchIDs) == 0 || game.points > result.BestMatch.Points {
			result.BestMatch = FantasyRecord{Points: round2(game.points), MatchIDs: []uint64{game.id}}
		}
	}
	series := make(map[uint64][]gameScore)
	for _, game := range games {
		series[game.seriesID] = append(series[game.seriesID], game)
	}
	for _, seriesGames := range series {
		sort.SliceStable(seriesGames, func(i, j int) bool {
			return seriesGames[i].points > seriesGames[j].points
		})
		if len(seriesGames) > 2 {
			seriesGames = seriesGames[:2]
		}
		record := FantasyRecord{}
		for _, game := range seriesGames {
			record.Points += game.points
			record.MatchIDs = append(record.MatchIDs, game.id)
		}
		record.Points = round2(record.Points)
		if len(result.BestSeries.MatchIDs) == 0 || record.Points > result.BestSeries.Points {
			result.BestSeries = record
		}
	}
	return result, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
