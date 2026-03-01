package economy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hildolfr/daz/internal/framework"
)

const (
	queryGetBalance = "SELECT * FROM daz_economy_get_balance($1,$2)"
	queryCredit     = "SELECT * FROM daz_economy_credit($1,$2,$3,$4,$5,$6,$7)"
	queryDebit      = "SELECT * FROM daz_economy_debit($1,$2,$3,$4,$5,$6,$7)"
	queryTransfer   = "SELECT * FROM daz_economy_transfer($1,$2,$3,$4,$5,$6,$7,$8)"
)

type Store interface {
	GetBalance(ctx context.Context, channel, username string) (GetBalanceResult, error)
	Credit(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (CreditResult, error)
	Debit(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (DebitResult, error)
	Transfer(ctx context.Context, channel, fromUsername, toUsername string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (TransferResult, error)
}

type GetBalanceResult struct {
	OK        bool
	ErrorCode string
	Balance   int64
}

type CreditResult struct {
	OK             bool
	ErrorCode      string
	Balance        int64
	LedgerID       int64
	AlreadyApplied bool
}

type DebitResult struct {
	OK             bool
	ErrorCode      string
	Balance        int64
	LedgerID       int64
	AlreadyApplied bool
}

type TransferResult struct {
	OK             bool
	ErrorCode      string
	FromBalance    int64
	ToBalance      int64
	LedgerID       int64
	AlreadyApplied bool
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

type sqlStore struct {
	helper *framework.SQLRequestHelper
}

func NewSQLStore(helper *framework.SQLRequestHelper) Store {
	return &sqlStore{helper: helper}
}

func (s *sqlStore) GetBalance(ctx context.Context, channel, username string) (GetBalanceResult, error) {
	rows, err := s.query(ctx, queryGetBalance, channel, username)
	if err != nil {
		return GetBalanceResult{}, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return GetBalanceResult{}, &StoreError{Code: errorCodeDBError, Err: errors.New("daz_economy_get_balance returned no rows")}
	}

	var ok bool
	var errorCode *string
	var balance int64
	if err := rows.Scan(&ok, &errorCode, &balance); err != nil {
		return GetBalanceResult{}, &StoreError{Code: errorCodeDBError, Err: fmt.Errorf("failed to scan daz_economy_get_balance row: %w", err)}
	}

	return GetBalanceResult{
		OK:        ok,
		ErrorCode: stringOrEmpty(errorCode),
		Balance:   balance,
	}, nil
}

func (s *sqlStore) Credit(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (CreditResult, error) {
	rows, err := s.query(ctx, queryCredit, channel, username, amount, idempotencyKey, actor, reason, metadata)
	if err != nil {
		return CreditResult{}, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return CreditResult{}, &StoreError{Code: errorCodeDBError, Err: errors.New("daz_economy_credit returned no rows")}
	}

	var ok bool
	var errorCode *string
	var balance int64
	var ledgerID *int64
	var alreadyApplied bool
	if err := rows.Scan(&ok, &errorCode, &balance, &ledgerID, &alreadyApplied); err != nil {
		return CreditResult{}, &StoreError{Code: errorCodeDBError, Err: fmt.Errorf("failed to scan daz_economy_credit row: %w", err)}
	}

	return CreditResult{
		OK:             ok,
		ErrorCode:      stringOrEmpty(errorCode),
		Balance:        balance,
		LedgerID:       int64OrZero(ledgerID),
		AlreadyApplied: alreadyApplied,
	}, nil
}

func (s *sqlStore) Debit(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (DebitResult, error) {
	rows, err := s.query(ctx, queryDebit, channel, username, amount, idempotencyKey, actor, reason, metadata)
	if err != nil {
		return DebitResult{}, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return DebitResult{}, &StoreError{Code: errorCodeDBError, Err: errors.New("daz_economy_debit returned no rows")}
	}

	var ok bool
	var errorCode *string
	var balance int64
	var ledgerID *int64
	var alreadyApplied bool
	if err := rows.Scan(&ok, &errorCode, &balance, &ledgerID, &alreadyApplied); err != nil {
		return DebitResult{}, &StoreError{Code: errorCodeDBError, Err: fmt.Errorf("failed to scan daz_economy_debit row: %w", err)}
	}

	return DebitResult{
		OK:             ok,
		ErrorCode:      stringOrEmpty(errorCode),
		Balance:        balance,
		LedgerID:       int64OrZero(ledgerID),
		AlreadyApplied: alreadyApplied,
	}, nil
}

func (s *sqlStore) Transfer(ctx context.Context, channel, fromUsername, toUsername string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (TransferResult, error) {
	rows, err := s.query(ctx, queryTransfer, channel, fromUsername, toUsername, amount, idempotencyKey, actor, reason, metadata)
	if err != nil {
		return TransferResult{}, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return TransferResult{}, &StoreError{Code: errorCodeDBError, Err: errors.New("daz_economy_transfer returned no rows")}
	}

	var ok bool
	var errorCode *string
	var fromBalance int64
	var toBalance int64
	var ledgerID *int64
	var alreadyApplied bool
	if err := rows.Scan(&ok, &errorCode, &fromBalance, &toBalance, &ledgerID, &alreadyApplied); err != nil {
		return TransferResult{}, &StoreError{Code: errorCodeDBError, Err: fmt.Errorf("failed to scan daz_economy_transfer row: %w", err)}
	}

	return TransferResult{
		OK:             ok,
		ErrorCode:      stringOrEmpty(errorCode),
		FromBalance:    fromBalance,
		ToBalance:      toBalance,
		LedgerID:       int64OrZero(ledgerID),
		AlreadyApplied: alreadyApplied,
	}, nil
}

func (s *sqlStore) query(ctx context.Context, query string, args ...interface{}) (*framework.QueryRows, error) {
	if s.helper == nil {
		return nil, &StoreError{Code: errorCodeDBUnavailable, Err: errors.New("sql helper not configured")}
	}

	rows, err := s.helper.NormalQuery(ctx, query, args...)
	if err != nil {
		return nil, mapSQLRequestError(err)
	}

	return rows, nil
}

func mapSQLRequestError(err error) error {
	if err == nil {
		return nil
	}

	lowerErr := strings.ToLower(err.Error())
	if strings.Contains(lowerErr, "query failed:") || strings.Contains(lowerErr, "scan") {
		return &StoreError{Code: errorCodeDBError, Err: err}
	}

	return &StoreError{Code: errorCodeDBUnavailable, Err: err}
}

func mapStoreErrorCode(err error) string {
	if err == nil {
		return ""
	}

	var storeErr *StoreError
	if errors.As(err, &storeErr) && storeErr.Code != "" {
		return storeErr.Code
	}

	return errorCodeInternal
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func int64OrZero(value *int64) int64 {
	if value == nil {
		return 0
	}

	return *value
}
