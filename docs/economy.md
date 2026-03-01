# Economy Plugin Contract (v1)

This document defines the stable request/response contract for the `economy` plugin over the EventBus `plugin.request` pattern.

It is intentionally narrow: operation names, payload shape, correlation, idempotency, and error codes.

## Transport

- Event type: `plugin.request`
- Request envelope: `framework.EventData.PluginRequest` (serialized as `plugin_request`)
- Response envelope: `framework.EventData.PluginResponse` (serialized as `plugin_response`)

The economy plugin routes requests using the payload only. Handlers do not receive `EventMetadata`, so anything required for correlation, idempotency, or semantics must be present in the request payload.

## Correlation rules (required)

The correlation ID for a request is `PluginRequest.ID`.

- `PluginRequest.ID` MUST be unique per logical request.
- The economy plugin MUST call `eventBus.DeliverResponse(req.ID, resp, err)`.
- If the caller uses `eventBus.Request(...)` (synchronous request/reply), it MUST also set `EventMetadata.CorrelationID` to the same value as `PluginRequest.ID`, otherwise the caller will wait on a correlation ID the handler cannot see.

## Idempotency rules (credit, debit, transfer)

Operations that change balances are idempotent.

- Request payload MAY include `idempotency_key`.
- If `idempotency_key` is omitted or empty, the server defaults it to `PluginRequest.ID`.
- If the same idempotency key is seen again with an identical payload, the operation is not applied again and the response sets `already_applied=true`.
- If the same idempotency key is seen again with a different payload, the request fails with `error_code=IDEMPOTENCY_CONFLICT`.

Clients should retry by re-sending the same JSON fields and values.

## Error codes

On failure, the response uses `PluginResponse.Success=false` and includes a deterministic `error_code` in `PluginResponse.Data.RawJSON`.

- `INVALID_ARGUMENT`: missing required fields, invalid types, invalid JSON for `data.raw_json`, unknown operation name.
- `INVALID_AMOUNT`: `amount` is missing, not an integer, or is less than or equal to 0.
- `INSUFFICIENT_FUNDS`: debit or transfer would make the sender balance negative.
- `IDEMPOTENCY_CONFLICT`: duplicate idempotency key with a different payload.
- `DB_UNAVAILABLE`: database layer is not ready (for example SQL plugin down) or requests cannot be executed.
- `DB_ERROR`: database operation failed after the DB layer was available.
- `INTERNAL`: any unexpected error.

## Request envelope

`PluginRequest` fields:

- `id` (string, required): correlation ID. See correlation rules.
- `from` (string, required): the sending plugin name.
- `to` (string, required): MUST be `"economy"`.
- `type` (string, required): operation name. See operations.
- `data.raw_json` (object, required): operation-specific request payload.

Example (shape only):

```json
{
  "plugin_request": {
    "id": "1719953890644416000",
    "from": "myplugin",
    "to": "economy",
    "type": "economy.get_balance",
    "data": {
      "raw_json": {
        "channel": "my-channel",
        "username": "alice"
      }
    }
  }
}
```

## Response envelope

`PluginResponse` fields:

- `id` (string, required): MUST equal the request `PluginRequest.ID`.
- `from` (string, required): MUST be `"economy"`.
- `success` (bool, required)
- `error` (string, optional): human-friendly message.
- `data.raw_json` (object, optional): operation result, or an error object.

Error payload shape:

```json
{
  "error_code": "INVALID_ARGUMENT",
  "message": "missing field channel",
  "details": {
    "field": "channel"
  }
}
```

## Operations

Operation names are stable and MUST match exactly:

- `economy.get_balance`
- `economy.credit`
- `economy.debit`
- `economy.transfer`

All operations are channel-scoped.

### economy.get_balance

Request `data.raw_json`:

- `channel` (string, required)
- `username` (string, required)

Response `data.raw_json` (success):

- `channel` (string)
- `username` (string)
- `balance` (integer)

Request example:

```json
{
  "plugin_request": {
    "id": "bal-1719953890644416000",
    "from": "myplugin",
    "to": "economy",
    "type": "economy.get_balance",
    "data": {
      "raw_json": {
        "channel": "my-channel",
        "username": "alice"
      }
    }
  }
}
```

Response example:

```json
{
  "plugin_response": {
    "id": "bal-1719953890644416000",
    "from": "economy",
    "success": true,
    "data": {
      "raw_json": {
        "channel": "my-channel",
        "username": "alice",
        "balance": 1250
      }
    }
  }
}
```

### economy.credit

Request `data.raw_json`:

- `channel` (string, required)
- `username` (string, required)
- `amount` (integer, required, must be greater than 0)
- `idempotency_key` (string, optional)
- `reason` (string, optional)
- `metadata` (object, optional)

Response `data.raw_json` (success):

- `channel` (string)
- `username` (string)
- `amount` (integer)
- `balance_before` (integer)
- `balance_after` (integer)
- `idempotency_key` (string)
- `already_applied` (bool)

Request example:

```json
{
  "plugin_request": {
    "id": "cred-1719953890644416000",
    "from": "myplugin",
    "to": "economy",
    "type": "economy.credit",
    "data": {
      "raw_json": {
        "channel": "my-channel",
        "username": "alice",
        "amount": 250,
        "idempotency_key": "txn-8f2d0d4a",
        "reason": "daily_reward",
        "metadata": {
          "source": "rewards",
          "campaign": "mar-2026"
        }
      }
    }
  }
}
```

Response example:

```json
{
  "plugin_response": {
    "id": "cred-1719953890644416000",
    "from": "economy",
    "success": true,
    "data": {
      "raw_json": {
        "channel": "my-channel",
        "username": "alice",
        "amount": 250,
        "balance_before": 1250,
        "balance_after": 1500,
        "idempotency_key": "txn-8f2d0d4a",
        "already_applied": false
      }
    }
  }
}
```

### economy.debit

Request `data.raw_json`:

- `channel` (string, required)
- `username` (string, required)
- `amount` (integer, required, must be greater than 0)
- `idempotency_key` (string, optional)
- `reason` (string, optional)
- `metadata` (object, optional)

Response `data.raw_json` (success):

- `channel` (string)
- `username` (string)
- `amount` (integer)
- `balance_before` (integer)
- `balance_after` (integer)
- `idempotency_key` (string)
- `already_applied` (bool)

Request example:

```json
{
  "plugin_request": {
    "id": "debit-1719953890644416000",
    "from": "myplugin",
    "to": "economy",
    "type": "economy.debit",
    "data": {
      "raw_json": {
        "channel": "my-channel",
        "username": "alice",
        "amount": 300,
        "reason": "shop_purchase",
        "metadata": {
          "item_id": "sword_01"
        }
      }
    }
  }
}
```

Response example (insufficient funds):

```json
{
  "plugin_response": {
    "id": "debit-1719953890644416000",
    "from": "economy",
    "success": false,
    "error": "insufficient funds",
    "data": {
      "raw_json": {
        "error_code": "INSUFFICIENT_FUNDS",
        "message": "debit would make balance negative",
        "details": {
          "channel": "my-channel",
          "username": "alice",
          "amount": 300
        }
      }
    }
  }
}
```

### economy.transfer

Request `data.raw_json`:

- `channel` (string, required)
- `from_username` (string, required)
- `to_username` (string, required)
- `amount` (integer, required, must be greater than 0)
- `idempotency_key` (string, optional)
- `reason` (string, optional)
- `metadata` (object, optional)

Response `data.raw_json` (success):

- `channel` (string)
- `from_username` (string)
- `to_username` (string)
- `amount` (integer)
- `from_balance_before` (integer)
- `from_balance_after` (integer)
- `to_balance_before` (integer)
- `to_balance_after` (integer)
- `idempotency_key` (string)
- `already_applied` (bool)

Request example:

```json
{
  "plugin_request": {
    "id": "xfer-1719953890644416000",
    "from": "myplugin",
    "to": "economy",
    "type": "economy.transfer",
    "data": {
      "raw_json": {
        "channel": "my-channel",
        "from_username": "alice",
        "to_username": "bob",
        "amount": 50,
        "idempotency_key": "pay-20260301-0001",
        "reason": "tip",
        "metadata": {
          "message_id": "chatmsg-001"
        }
      }
    }
  }
}
```

Response example:

```json
{
  "plugin_response": {
    "id": "xfer-1719953890644416000",
    "from": "economy",
    "success": true,
    "data": {
      "raw_json": {
        "channel": "my-channel",
        "from_username": "alice",
        "to_username": "bob",
        "amount": 50,
        "from_balance_before": 1500,
        "from_balance_after": 1450,
        "to_balance_before": 20,
        "to_balance_after": 70,
        "idempotency_key": "pay-20260301-0001",
        "already_applied": false
      }
    }
  }
}
```
