package pissingcontest

import (
	"context"
	"fmt"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type store struct {
	db *framework.SQLClient
}

func newStore(sqlClient *framework.SQLClient) *store {
	return &store{db: sqlClient}
}

func (s *store) getBladderState(ctx context.Context, channel, username string) (int64, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT current_amount FROM daz_user_bladder WHERE channel = $1 AND username = $2`,
		channel, username,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	if !rows.Next() {
		return 0, nil
	}

	var amount int64
	if err := rows.Scan(&amount); err != nil {
		return 0, err
	}
	return amount, nil
}

func (s *store) resetBladder(ctx context.Context, channel, username string) error {
	_, err := s.db.ExecContext(
		ctx,
		`
			INSERT INTO daz_user_bladder (channel, username, current_amount, last_piss_time, created_at, updated_at)
			VALUES ($1, $2, 0, NOW(), NOW(), NOW())
			ON CONFLICT (channel, username) DO UPDATE
			SET current_amount = 0,
			    last_piss_time = NOW(),
			    updated_at = NOW()
		`,
		channel, username,
	)
	return err
}

func (s *store) recordContestStats(ctx context.Context, channel, winner, loser string, winnerScore, loserScore int, winnerStats, loserStats contestResult, amount int64) error {
	if winner == "" && loser == "" {
		return nil
	}

	if winner != "" {
		if err := s.updateUserStats(ctx, channel, winner, true, amount, winnerStats, winnerScore); err != nil {
			return err
		}
	}
	if loser != "" {
		if err := s.updateUserStats(ctx, channel, loser, false, amount, loserStats, loserScore); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) updateUserStats(
	ctx context.Context,
	channel, username string,
	won bool,
	amount int64,
	stats contestResult,
	score int,
) error {
	wins := boolToInt(won)
	losses := 1 - wins
	moneyWon := int64(0)
	moneyLost := int64(0)
	if won {
		moneyWon = amount
	} else {
		moneyLost = amount
	}

	_, err := s.db.ExecContext(
		ctx,
		`
				INSERT INTO daz_pissing_contest_stats
				(
					channel, username, total_matches, wins, losses, money_won, money_lost,
					best_distance, best_volume, best_aim, best_duration,
					avg_distance, avg_volume, avg_aim, avg_duration,
					last_played_at, updated_at
				)
				VALUES
				($1, $2, 1, $3, $4, $5, $6,
				 $7, $8, $9, $10,
				 $7, $8, $9, $10,
				 NOW(), NOW())
				ON CONFLICT (channel, username) DO UPDATE SET
					total_matches = daz_pissing_contest_stats.total_matches + 1,
					wins = daz_pissing_contest_stats.wins + $3,
					losses = daz_pissing_contest_stats.losses + $4,
					money_won = daz_pissing_contest_stats.money_won + $5,
					money_lost = daz_pissing_contest_stats.money_lost + $6,
					best_distance = GREATEST(daz_pissing_contest_stats.best_distance, $7),
					best_volume = GREATEST(daz_pissing_contest_stats.best_volume, $8),
					best_aim = GREATEST(daz_pissing_contest_stats.best_aim, $9),
					best_duration = GREATEST(daz_pissing_contest_stats.best_duration, $10),
					avg_distance = (daz_pissing_contest_stats.avg_distance * daz_pissing_contest_stats.total_matches + $7) / (daz_pissing_contest_stats.total_matches + 1),
					avg_volume = (daz_pissing_contest_stats.avg_volume * daz_pissing_contest_stats.total_matches + $8) / (daz_pissing_contest_stats.total_matches + 1),
					avg_aim = (daz_pissing_contest_stats.avg_aim * daz_pissing_contest_stats.total_matches + $9) / (daz_pissing_contest_stats.total_matches + 1),
					avg_duration = (daz_pissing_contest_stats.avg_duration * daz_pissing_contest_stats.total_matches + $10) / (daz_pissing_contest_stats.total_matches + 1),
					last_played_at = NOW(),
					updated_at = NOW()
			`,
		channel, username, wins, losses, moneyWon, moneyLost,
		stats.distance, stats.volume, stats.aim, stats.duration,
	)
	return err
}

func (s *store) saveCompletedMatch(ctx context.Context, ch *activeChallenge) error {
	if ch == nil {
		return nil
	}

	_, err := s.db.ExecContext(
		ctx,
		`
			INSERT INTO daz_pissing_contest_challenges
			(
				channel, challenger, challenged, amount, status,
				challenger_characteristic, challenged_characteristic, location, weather,
				challenger_distance, challenger_volume, challenger_aim, challenger_duration, challenger_total,
				challenged_distance, challenged_volume, challenged_aim, challenged_duration, challenged_total,
				winner, created_at, expires_at, completed_at
			)
			VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
		`,
		ch.Room,
		ch.Challenger,
		ch.Challenged,
		ch.Amount,
		completedChallengeStat,
		ch.ChallengerChar.Name,
		ch.ChallengedChar.Name,
		ch.Location.Name,
		formatWeather(ch.Weather),
		ch.ChallengerStats.distance,
		ch.ChallengerStats.volume,
		ch.ChallengerStats.aim,
		ch.ChallengerStats.duration,
		ch.ChallengerScore,
		ch.ChallengedStats.distance,
		ch.ChallengedStats.volume,
		ch.ChallengedStats.aim,
		ch.ChallengedStats.duration,
		ch.ChallengedScore,
		chainWinner(ch),
		ch.CreatedAt,
		ch.ExpiresAt,
		time.Now(),
	)
	return err
}

func chainWinner(ch *activeChallenge) string {
	switch {
	case ch == nil:
		return ""
	case ch.ChallengerScore > ch.ChallengedScore:
		return ch.Challenger
	case ch.ChallengedScore > ch.ChallengerScore:
		return ch.Challenged
	default:
		return ""
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *store) logBalanceError(err error) error {
	return fmt.Errorf("store write failed: %w", err)
}
