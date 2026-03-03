package playerstate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hildolfr/daz/internal/framework"
)

const (
	queryGetPlayerState = "SELECT bladder, alcohol, weed, food, lust FROM daz_player_state WHERE channel = $1 AND username = $2"
	querySetPlayerState = `INSERT INTO daz_player_state (channel, username, bladder, alcohol, weed, food, lust, created_at, updated_at, last_bladder_mutation_at, last_alcohol_mutation_at, last_weed_mutation_at, last_food_mutation_at, last_lust_mutation_at)
		 VALUES ($1, $2, GREATEST(COALESCE($3, 0), 0), GREATEST(COALESCE($4, 0), 0), GREATEST(COALESCE($5, 0), 0), GREATEST(COALESCE($6, 0), 0), GREATEST(COALESCE($7, 0), 0), NOW(), NOW(), CASE WHEN $3 IS NOT NULL THEN NOW() ELSE NULL END, CASE WHEN $4 IS NOT NULL THEN NOW() ELSE NULL END, CASE WHEN $5 IS NOT NULL THEN NOW() ELSE NULL END, CASE WHEN $6 IS NOT NULL THEN NOW() ELSE NULL END, CASE WHEN $7 IS NOT NULL THEN NOW() ELSE NULL END)
		 ON CONFLICT (channel, username) DO UPDATE SET
			bladder = GREATEST(COALESCE($3, daz_player_state.bladder), 0),
			alcohol = GREATEST(COALESCE($4, daz_player_state.alcohol), 0),
			weed = GREATEST(COALESCE($5, daz_player_state.weed), 0),
			food = GREATEST(COALESCE($6, daz_player_state.food), 0),
			lust = GREATEST(COALESCE($7, daz_player_state.lust), 0),
			updated_at = NOW(),
			last_bladder_mutation_at = CASE WHEN $3 IS NOT NULL THEN NOW() ELSE daz_player_state.last_bladder_mutation_at END,
			last_alcohol_mutation_at = CASE WHEN $4 IS NOT NULL THEN NOW() ELSE daz_player_state.last_alcohol_mutation_at END,
			last_weed_mutation_at = CASE WHEN $5 IS NOT NULL THEN NOW() ELSE daz_player_state.last_weed_mutation_at END,
			last_food_mutation_at = CASE WHEN $6 IS NOT NULL THEN NOW() ELSE daz_player_state.last_food_mutation_at END,
			last_lust_mutation_at = CASE WHEN $7 IS NOT NULL THEN NOW() ELSE daz_player_state.last_lust_mutation_at END
		 RETURNING bladder, alcohol, weed, food, lust`
	queryAdjustPlayerState = `INSERT INTO daz_player_state (channel, username, bladder, alcohol, weed, food, lust, created_at, updated_at, last_bladder_mutation_at, last_alcohol_mutation_at, last_weed_mutation_at, last_food_mutation_at, last_lust_mutation_at)
		 VALUES ($1, $2, 0, 0, 0, 0, 0, NOW(), NOW(), CASE WHEN $3 IS NOT NULL THEN NOW() ELSE NULL END, CASE WHEN $4 IS NOT NULL THEN NOW() ELSE NULL END, CASE WHEN $5 IS NOT NULL THEN NOW() ELSE NULL END, CASE WHEN $6 IS NOT NULL THEN NOW() ELSE NULL END, CASE WHEN $7 IS NOT NULL THEN NOW() ELSE NULL END)
		 ON CONFLICT (channel, username) DO UPDATE SET
			bladder = GREATEST(daz_player_state.bladder + COALESCE($3, 0), 0),
			alcohol = GREATEST(daz_player_state.alcohol + COALESCE($4, 0), 0),
			weed = GREATEST(daz_player_state.weed + COALESCE($5, 0), 0),
			food = GREATEST(daz_player_state.food + COALESCE($6, 0), 0),
			lust = GREATEST(daz_player_state.lust + COALESCE($7, 0), 0),
			updated_at = NOW(),
			last_bladder_mutation_at = CASE WHEN $3 IS NOT NULL THEN NOW() ELSE daz_player_state.last_bladder_mutation_at END,
			last_alcohol_mutation_at = CASE WHEN $4 IS NOT NULL THEN NOW() ELSE daz_player_state.last_alcohol_mutation_at END,
			last_weed_mutation_at = CASE WHEN $5 IS NOT NULL THEN NOW() ELSE daz_player_state.last_weed_mutation_at END,
			last_food_mutation_at = CASE WHEN $6 IS NOT NULL THEN NOW() ELSE daz_player_state.last_food_mutation_at END,
			last_lust_mutation_at = CASE WHEN $7 IS NOT NULL THEN NOW() ELSE daz_player_state.last_lust_mutation_at END
		 RETURNING bladder, alcohol, weed, food, lust`
	queryEnsurePlayerState = `INSERT INTO daz_player_state (channel, username, created_at, updated_at)
		 VALUES ($1, $2, NOW(), NOW())
		 ON CONFLICT (channel, username) DO NOTHING`
	errorCodeStoreDBUnavailable = "DB_UNAVAILABLE"
	errorCodeStoreDBError       = "DB_ERROR"
)

type Store interface {
	Get(ctx context.Context, channel, username string) (PlayerState, error)
	Set(ctx context.Context, channel, username string, state PlayerState) (PlayerState, error)
	Adjust(ctx context.Context, channel, username string, delta PlayerState) (PlayerState, error)
	SetFields(ctx context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error)
	AdjustFields(ctx context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error)
}

type store struct {
	db *framework.SQLClient
}

type StoreError struct {
	Code string
	Err  error
}

func (e *StoreError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Code
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewStore(sqlClient *framework.SQLClient) Store {
	return &store{db: sqlClient}
}

func (s *store) Get(ctx context.Context, channel, username string) (PlayerState, error) {
	if s.db == nil {
		return PlayerState{}, &StoreError{
			Code: errorCodeStoreDBUnavailable,
			Err:  errors.New("sql client not configured"),
		}
	}

	if _, err := s.db.ExecContext(ctx,
		queryEnsurePlayerState,
		channel,
		username,
	); err != nil {
		return PlayerState{}, mapStoreError(err)
	}

	rows, err := s.db.QueryContext(ctx, queryGetPlayerState, channel, username)
	if err != nil {
		return PlayerState{}, mapStoreError(err)
	}
	defer rows.Close()

	if !rows.Next() {
		return PlayerState{}, &StoreError{Code: errorCodeStoreDBError, Err: fmt.Errorf("player state missing")}
	}

	var state PlayerState
	if err := rows.Scan(&state.Bladder, &state.Alcohol, &state.Weed, &state.Food, &state.Lust); err != nil {
		return PlayerState{}, mapStoreError(err)
	}

	return state, nil
}

func (s *store) Set(ctx context.Context, channel, username string, state PlayerState) (PlayerState, error) {
	bladder := state.Bladder
	alcohol := state.Alcohol
	weed := state.Weed
	food := state.Food
	lust := state.Lust
	return s.set(ctx, channel, username, &bladder, &alcohol, &weed, &food, &lust)
}

func (s *store) Adjust(ctx context.Context, channel, username string, delta PlayerState) (PlayerState, error) {
	bladder := delta.Bladder
	alcohol := delta.Alcohol
	weed := delta.Weed
	food := delta.Food
	lust := delta.Lust
	return s.adjust(ctx, channel, username, &bladder, &alcohol, &weed, &food, &lust)
}

func (s *store) SetFields(ctx context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error) {
	return s.set(ctx, channel, username, bladder, alcohol, weed, food, lust)
}

func (s *store) AdjustFields(ctx context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error) {
	return s.adjust(ctx, channel, username, bladder, alcohol, weed, food, lust)
}

func (s *store) set(
	ctx context.Context,
	channel, username string,
	bladder, alcohol, weed, food, lust *int64,
) (PlayerState, error) {
	if s.db == nil {
		return PlayerState{}, &StoreError{
			Code: errorCodeStoreDBUnavailable,
			Err:  errors.New("sql client not configured"),
		}
	}

	rows, err := s.db.QueryContext(ctx,
		querySetPlayerState,
		channel, username, valueOrNil(bladder), valueOrNil(alcohol), valueOrNil(weed), valueOrNil(food), valueOrNil(lust),
	)
	if err != nil {
		return PlayerState{}, mapStoreError(err)
	}
	defer rows.Close()

	return scanPlayerState(rows)
}

func (s *store) adjust(
	ctx context.Context,
	channel, username string,
	bladder, alcohol, weed, food, lust *int64,
) (PlayerState, error) {
	if s.db == nil {
		return PlayerState{}, &StoreError{
			Code: errorCodeStoreDBUnavailable,
			Err:  errors.New("sql client not configured"),
		}
	}

	rows, err := s.db.QueryContext(ctx,
		queryAdjustPlayerState,
		channel, username, valueOrNil(bladder), valueOrNil(alcohol), valueOrNil(weed), valueOrNil(food), valueOrNil(lust),
	)
	if err != nil {
		return PlayerState{}, mapStoreError(err)
	}
	defer rows.Close()

	return scanPlayerState(rows)
}

func scanPlayerState(rows *framework.QueryRows) (PlayerState, error) {
	var state PlayerState
	if !rows.Next() {
		return state, fmt.Errorf("player state row missing")
	}

	if err := rows.Scan(&state.Bladder, &state.Alcohol, &state.Weed, &state.Food, &state.Lust); err != nil {
		return PlayerState{}, err
	}

	return state, nil
}

func valueOrNil(value *int64) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}

	var storeErr *StoreError
	if errors.As(err, &storeErr) {
		return storeErr
	}

	lowerErr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lowerErr, "query failed"):
		fallthrough
	case strings.Contains(lowerErr, "exec failed"):
		fallthrough
	case strings.Contains(lowerErr, "no response received"):
		fallthrough
	case strings.Contains(lowerErr, "query request failed"):
		fallthrough
	case strings.Contains(lowerErr, "exec request failed"):
		fallthrough
	case strings.Contains(lowerErr, "scan"):
		return &StoreError{Code: errorCodeStoreDBError, Err: err}
	default:
		return &StoreError{Code: errorCodeStoreDBUnavailable, Err: err}
	}
}
