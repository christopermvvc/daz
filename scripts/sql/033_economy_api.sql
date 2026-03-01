CREATE TABLE IF NOT EXISTS daz_economy_ledger (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('credit', 'debit', 'transfer')),
    actor VARCHAR(255),
    from_username VARCHAR(255),
    to_username VARCHAR(255),
    amount BIGINT NOT NULL CHECK (amount > 0),
    reason TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    idempotency_key VARCHAR(255),
    balance_after BIGINT,
    from_balance_after BIGINT,
    to_balance_after BIGINT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_economy_ledger_channel_created_at
    ON daz_economy_ledger(channel, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_economy_ledger_channel_from_username
    ON daz_economy_ledger(channel, from_username, created_at DESC)
    WHERE from_username IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_economy_ledger_channel_to_username
    ON daz_economy_ledger(channel, to_username, created_at DESC)
    WHERE to_username IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_economy_ledger_channel_idempotency
    ON daz_economy_ledger(channel, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE OR REPLACE FUNCTION daz_economy_get_balance(
    p_channel VARCHAR(255),
    p_username VARCHAR(255)
) RETURNS TABLE (
    ok BOOLEAN,
    error_code TEXT,
    balance BIGINT
) AS $$
DECLARE
    v_channel VARCHAR(255);
    v_username VARCHAR(255);
BEGIN
    v_channel := TRIM(COALESCE(p_channel, ''));
    v_username := LOWER(TRIM(COALESCE(p_username, '')));

    IF v_channel = '' THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT;
        RETURN;
    END IF;

    IF v_username = '' THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT;
        RETURN;
    END IF;

    INSERT INTO daz_user_economy (channel, username, balance, total_earned, total_lost)
    VALUES (v_channel, v_username, 0, 0, 0)
    ON CONFLICT (channel, username) DO NOTHING;

    RETURN QUERY
    SELECT TRUE, NULL::TEXT, ue.balance
    FROM daz_user_economy ue
    WHERE ue.channel = v_channel
      AND ue.username = v_username;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION daz_economy_credit(
    p_channel VARCHAR(255),
    p_username VARCHAR(255),
    p_amount BIGINT,
    p_idempotency_key VARCHAR(255),
    p_actor VARCHAR(255),
    p_reason TEXT,
    p_metadata JSONB
) RETURNS TABLE (
    ok BOOLEAN,
    error_code TEXT,
    balance BIGINT,
    ledger_id BIGINT,
    already_applied BOOLEAN
) AS $$
DECLARE
    v_channel VARCHAR(255);
    v_username VARCHAR(255);
    v_key VARCHAR(255);
    v_actor VARCHAR(255);
    v_reason TEXT;
    v_metadata JSONB;
    v_balance BIGINT;
    v_ledger_id BIGINT;
    v_existing RECORD;
BEGIN
    v_channel := TRIM(COALESCE(p_channel, ''));
    v_username := LOWER(TRIM(COALESCE(p_username, '')));
    v_key := NULLIF(TRIM(COALESCE(p_idempotency_key, '')), '');
    v_actor := NULLIF(TRIM(COALESCE(p_actor, '')), '');
    v_reason := NULLIF(TRIM(COALESCE(p_reason, '')), '');
    v_metadata := COALESCE(p_metadata, '{}'::jsonb);

    IF v_channel = '' THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF v_username = '' THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF p_amount IS NULL OR p_amount <= 0 THEN
        RETURN QUERY SELECT FALSE, 'INVALID_AMOUNT', 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF v_key IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(v_channel || ':' || v_key, 0));

        SELECT l.id,
               l.kind,
               l.actor,
               l.from_username,
               l.to_username,
               l.amount,
               l.reason,
               l.metadata,
               l.balance_after
        INTO v_existing
        FROM daz_economy_ledger l
        WHERE l.channel = v_channel
          AND l.idempotency_key = v_key
        LIMIT 1;

        IF FOUND THEN
            IF v_existing.kind = 'credit'
               AND v_existing.to_username = v_username
               AND v_existing.amount = p_amount
               AND COALESCE(v_existing.actor, '') = COALESCE(v_actor, '')
               AND COALESCE(v_existing.reason, '') = COALESCE(v_reason, '')
               AND COALESCE(v_existing.metadata, '{}'::jsonb) = v_metadata THEN
                RETURN QUERY
                SELECT TRUE, NULL::TEXT, v_existing.balance_after, v_existing.id, TRUE;
                RETURN;
            END IF;

            RETURN QUERY SELECT FALSE, 'IDEMPOTENCY_CONFLICT', 0::BIGINT, NULL::BIGINT, FALSE;
            RETURN;
        END IF;
    END IF;

    INSERT INTO daz_user_economy (channel, username, balance, total_earned, total_lost)
    VALUES (v_channel, v_username, 0, 0, 0)
    ON CONFLICT (channel, username) DO NOTHING;

    PERFORM 1
    FROM daz_user_economy ue
    WHERE ue.channel = v_channel
      AND ue.username = v_username
    FOR UPDATE;

    UPDATE daz_user_economy ue
    SET balance = ue.balance + p_amount,
        total_earned = ue.total_earned + p_amount,
        updated_at = NOW()
    WHERE ue.channel = v_channel
      AND ue.username = v_username
    RETURNING ue.balance INTO v_balance;

    INSERT INTO daz_economy_ledger (
        channel,
        kind,
        actor,
        to_username,
        amount,
        reason,
        metadata,
        idempotency_key,
        balance_after
    ) VALUES (
        v_channel,
        'credit',
        v_actor,
        v_username,
        p_amount,
        v_reason,
        v_metadata,
        v_key,
        v_balance
    )
    RETURNING id INTO v_ledger_id;

    RETURN QUERY SELECT TRUE, NULL::TEXT, v_balance, v_ledger_id, FALSE;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION daz_economy_debit(
    p_channel VARCHAR(255),
    p_username VARCHAR(255),
    p_amount BIGINT,
    p_idempotency_key VARCHAR(255),
    p_actor VARCHAR(255),
    p_reason TEXT,
    p_metadata JSONB
) RETURNS TABLE (
    ok BOOLEAN,
    error_code TEXT,
    balance BIGINT,
    ledger_id BIGINT,
    already_applied BOOLEAN
) AS $$
DECLARE
    v_channel VARCHAR(255);
    v_username VARCHAR(255);
    v_key VARCHAR(255);
    v_actor VARCHAR(255);
    v_reason TEXT;
    v_metadata JSONB;
    v_current_balance BIGINT;
    v_balance BIGINT;
    v_ledger_id BIGINT;
    v_existing RECORD;
BEGIN
    v_channel := TRIM(COALESCE(p_channel, ''));
    v_username := LOWER(TRIM(COALESCE(p_username, '')));
    v_key := NULLIF(TRIM(COALESCE(p_idempotency_key, '')), '');
    v_actor := NULLIF(TRIM(COALESCE(p_actor, '')), '');
    v_reason := NULLIF(TRIM(COALESCE(p_reason, '')), '');
    v_metadata := COALESCE(p_metadata, '{}'::jsonb);

    IF v_channel = '' THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF v_username = '' THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF p_amount IS NULL OR p_amount <= 0 THEN
        RETURN QUERY SELECT FALSE, 'INVALID_AMOUNT', 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF v_key IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(v_channel || ':' || v_key, 0));

        SELECT l.id,
               l.kind,
               l.actor,
               l.from_username,
               l.to_username,
               l.amount,
               l.reason,
               l.metadata,
               l.balance_after
        INTO v_existing
        FROM daz_economy_ledger l
        WHERE l.channel = v_channel
          AND l.idempotency_key = v_key
        LIMIT 1;

        IF FOUND THEN
            IF v_existing.kind = 'debit'
               AND v_existing.from_username = v_username
               AND v_existing.amount = p_amount
               AND COALESCE(v_existing.actor, '') = COALESCE(v_actor, '')
               AND COALESCE(v_existing.reason, '') = COALESCE(v_reason, '')
               AND COALESCE(v_existing.metadata, '{}'::jsonb) = v_metadata THEN
                RETURN QUERY
                SELECT TRUE, NULL::TEXT, v_existing.balance_after, v_existing.id, TRUE;
                RETURN;
            END IF;

            RETURN QUERY SELECT FALSE, 'IDEMPOTENCY_CONFLICT', 0::BIGINT, NULL::BIGINT, FALSE;
            RETURN;
        END IF;
    END IF;

    INSERT INTO daz_user_economy (channel, username, balance, total_earned, total_lost)
    VALUES (v_channel, v_username, 0, 0, 0)
    ON CONFLICT (channel, username) DO NOTHING;

    SELECT ue.balance
    INTO v_current_balance
    FROM daz_user_economy ue
    WHERE ue.channel = v_channel
      AND ue.username = v_username
    FOR UPDATE;

    IF v_current_balance < p_amount THEN
        RETURN QUERY SELECT FALSE, 'INSUFFICIENT_FUNDS', v_current_balance, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    UPDATE daz_user_economy ue
    SET balance = ue.balance - p_amount,
        total_lost = ue.total_lost + p_amount,
        updated_at = NOW()
    WHERE ue.channel = v_channel
      AND ue.username = v_username
    RETURNING ue.balance INTO v_balance;

    INSERT INTO daz_economy_ledger (
        channel,
        kind,
        actor,
        from_username,
        amount,
        reason,
        metadata,
        idempotency_key,
        balance_after
    ) VALUES (
        v_channel,
        'debit',
        v_actor,
        v_username,
        p_amount,
        v_reason,
        v_metadata,
        v_key,
        v_balance
    )
    RETURNING id INTO v_ledger_id;

    RETURN QUERY SELECT TRUE, NULL::TEXT, v_balance, v_ledger_id, FALSE;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION daz_economy_transfer(
    p_channel VARCHAR(255),
    p_from_username VARCHAR(255),
    p_to_username VARCHAR(255),
    p_amount BIGINT,
    p_idempotency_key VARCHAR(255),
    p_actor VARCHAR(255),
    p_reason TEXT,
    p_metadata JSONB
) RETURNS TABLE (
    ok BOOLEAN,
    error_code TEXT,
    from_balance BIGINT,
    to_balance BIGINT,
    ledger_id BIGINT,
    already_applied BOOLEAN
) AS $$
DECLARE
    v_channel VARCHAR(255);
    v_from_username VARCHAR(255);
    v_to_username VARCHAR(255);
    v_first_username VARCHAR(255);
    v_second_username VARCHAR(255);
    v_key VARCHAR(255);
    v_actor VARCHAR(255);
    v_reason TEXT;
    v_metadata JSONB;
    v_from_current BIGINT;
    v_from_balance BIGINT;
    v_to_balance BIGINT;
    v_ledger_id BIGINT;
    v_existing RECORD;
BEGIN
    v_channel := TRIM(COALESCE(p_channel, ''));
    v_from_username := LOWER(TRIM(COALESCE(p_from_username, '')));
    v_to_username := LOWER(TRIM(COALESCE(p_to_username, '')));
    v_key := NULLIF(TRIM(COALESCE(p_idempotency_key, '')), '');
    v_actor := NULLIF(TRIM(COALESCE(p_actor, '')), '');
    v_reason := NULLIF(TRIM(COALESCE(p_reason, '')), '');
    v_metadata := COALESCE(p_metadata, '{}'::jsonb);

    IF v_channel = '' THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT, 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF v_from_username = '' OR v_to_username = '' THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT, 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF v_from_username = v_to_username THEN
        RETURN QUERY SELECT FALSE, 'INVALID_ARGUMENT', 0::BIGINT, 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF p_amount IS NULL OR p_amount <= 0 THEN
        RETURN QUERY SELECT FALSE, 'INVALID_AMOUNT', 0::BIGINT, 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    IF v_key IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(v_channel || ':' || v_key, 0));

        SELECT l.id,
               l.kind,
               l.actor,
               l.from_username,
               l.to_username,
               l.amount,
               l.reason,
               l.metadata,
               l.from_balance_after,
               l.to_balance_after
        INTO v_existing
        FROM daz_economy_ledger l
        WHERE l.channel = v_channel
          AND l.idempotency_key = v_key
        LIMIT 1;

        IF FOUND THEN
            IF v_existing.kind = 'transfer'
               AND v_existing.from_username = v_from_username
               AND v_existing.to_username = v_to_username
               AND v_existing.amount = p_amount
               AND COALESCE(v_existing.actor, '') = COALESCE(v_actor, '')
               AND COALESCE(v_existing.reason, '') = COALESCE(v_reason, '')
               AND COALESCE(v_existing.metadata, '{}'::jsonb) = v_metadata THEN
                RETURN QUERY
                SELECT TRUE, NULL::TEXT, v_existing.from_balance_after, v_existing.to_balance_after, v_existing.id, TRUE;
                RETURN;
            END IF;

            RETURN QUERY SELECT FALSE, 'IDEMPOTENCY_CONFLICT', 0::BIGINT, 0::BIGINT, NULL::BIGINT, FALSE;
            RETURN;
        END IF;
    END IF;

    INSERT INTO daz_user_economy (channel, username, balance, total_earned, total_lost)
    VALUES (v_channel, v_from_username, 0, 0, 0)
    ON CONFLICT (channel, username) DO NOTHING;

    INSERT INTO daz_user_economy (channel, username, balance, total_earned, total_lost)
    VALUES (v_channel, v_to_username, 0, 0, 0)
    ON CONFLICT (channel, username) DO NOTHING;

    v_first_username := LEAST(v_from_username, v_to_username);
    v_second_username := GREATEST(v_from_username, v_to_username);

    PERFORM 1
    FROM daz_user_economy ue
    WHERE ue.channel = v_channel
      AND ue.username = v_first_username
    FOR UPDATE;

    PERFORM 1
    FROM daz_user_economy ue
    WHERE ue.channel = v_channel
      AND ue.username = v_second_username
    FOR UPDATE;

    SELECT ue.balance
    INTO v_from_current
    FROM daz_user_economy ue
    WHERE ue.channel = v_channel
      AND ue.username = v_from_username;

    IF v_from_current < p_amount THEN
        RETURN QUERY SELECT FALSE, 'INSUFFICIENT_FUNDS', v_from_current, 0::BIGINT, NULL::BIGINT, FALSE;
        RETURN;
    END IF;

    UPDATE daz_user_economy ue
    SET balance = ue.balance - p_amount,
        total_lost = ue.total_lost + p_amount,
        updated_at = NOW()
    WHERE ue.channel = v_channel
      AND ue.username = v_from_username
    RETURNING ue.balance INTO v_from_balance;

    UPDATE daz_user_economy ue
    SET balance = ue.balance + p_amount,
        total_earned = ue.total_earned + p_amount,
        updated_at = NOW()
    WHERE ue.channel = v_channel
      AND ue.username = v_to_username
    RETURNING ue.balance INTO v_to_balance;

    INSERT INTO daz_economy_ledger (
        channel,
        kind,
        actor,
        from_username,
        to_username,
        amount,
        reason,
        metadata,
        idempotency_key,
        from_balance_after,
        to_balance_after
    ) VALUES (
        v_channel,
        'transfer',
        v_actor,
        v_from_username,
        v_to_username,
        p_amount,
        v_reason,
        v_metadata,
        v_key,
        v_from_balance,
        v_to_balance
    )
    RETURNING id INTO v_ledger_id;

    RETURN QUERY SELECT TRUE, NULL::TEXT, v_from_balance, v_to_balance, v_ledger_id, FALSE;
END;
$$ LANGUAGE plpgsql;

GRANT SELECT, INSERT, UPDATE, DELETE ON daz_economy_ledger TO daz_user;
GRANT USAGE ON SEQUENCE daz_economy_ledger_id_seq TO daz_user;
GRANT EXECUTE ON FUNCTION daz_economy_get_balance(VARCHAR, VARCHAR) TO daz_user;
GRANT EXECUTE ON FUNCTION daz_economy_credit(VARCHAR, VARCHAR, BIGINT, VARCHAR, VARCHAR, TEXT, JSONB) TO daz_user;
GRANT EXECUTE ON FUNCTION daz_economy_debit(VARCHAR, VARCHAR, BIGINT, VARCHAR, VARCHAR, TEXT, JSONB) TO daz_user;
GRANT EXECUTE ON FUNCTION daz_economy_transfer(VARCHAR, VARCHAR, VARCHAR, BIGINT, VARCHAR, VARCHAR, TEXT, JSONB) TO daz_user;
