# WebSocket protocol

Connect:

```
ws://localhost:8080/api/v1/ws?token=<access_token>
```

The same endpoint is also available at `ws://localhost:8080/ws`. Native mobile
clients should use the query parameter. Non-browser clients may instead send
`Authorization: Bearer <access_token>`.

Close codes / handshake errors: `401 missing_token`, `401 invalid_token`,
`403 account_inactive`.

## Envelope

Server → client:

```json
{ "type": "message.new", "data": { }, "ts": "2026-01-01T12:00:00Z" }
```

Client → server:

```json
{ "type": "message.send", "request_id": "client-123", "data": { } }
```

`request_id` is echoed in acknowledgements and errors so the client can correlate.

## Client → server events

### `ping`

```json
{"type":"ping"}
```
→ `pong` with `server_time`.

### `message.send`

```json
{
  "type": "message.send",
  "request_id": "m-1",
  "data": {
    "conversation_id": "0f7d…",
    "body": "Привет!",
    "media_url": null
  }
}
```
→ `message.ack` with the stored message, and `message.new` to both participants.

### `typing`

```json
{"type":"typing","data":{"conversation_id":"0f7d…"}}
```
→ peer receives `typing` (rate-limited to one event/second per conversation).

### `message.read`

```json
{"type":"message.read","data":{"conversation_id":"0f7d…"}}
```
→ `message.read.ack`; the sender receives `message.read`.

## Server → client events

| Type | When | Payload |
|---|---|---|
| `ready` | After upgrade | `user_id`, `server_time` |
| `pong` | Reply to `ping` | `server_time` |
| `message.ack` | Message stored via WS | `request_id`, `message` |
| `message.new` | New message (REST or WS) | `conversation_id`, `message` |
| `gift.sent` | You sent a coin gift | `conversation_id`, `message`, `gift` |
| `gift.received` | Peer sent you a coin gift | `conversation_id`, `message`, `gift` |
| `message.read` | Peer read your messages | `conversation_id`, `reader_id`, `read_at` |
| `message.read.ack` | Your read request processed | `request_id`, `read_at` |
| `typing` | Peer is typing | `conversation_id`, `user_id`, `display_name` |
| `match.new` | New mutual like | `match_id`, `conversation_id`, `peer_id`, `peer_name`, `peer_photo` |
| `notification.new` | In-app notification created | notification object |
| `error` | Validation/domain error | `code`, `message`, `request_id` |

## Error codes

`invalid_event`, `invalid_data`, `invalid_conversation`, `not_found`,
`forbidden`, `conversation_closed`, `blocked`, `rate_limited`,
`validation_error`, `bad_request`.

## Presence

- On connect the hub sets `presence:<user_id>` in Redis with a 90-second TTL.
- Client pongs and inbound messages refresh the TTL.
- On disconnect the key is removed.
- Discovery/chat responses include `online` when the peer allows it.

## Multi-instance fan-out

Events are published to the Redis channel `love:ws:events` with the recipient
list. Each instance delivers only to its local clients. If Redis is unavailable,
delivery falls back to the local hub.

## Example session (wscat)

```bash
wscat -c "ws://localhost:8080/api/v1/ws?token=$TOKEN"
> {"type":"ping"}
< {"type":"pong","data":{"server_time":"…"},"ts":"…"}
> {"type":"message.send","request_id":"1","data":{"conversation_id":"…","body":"Привет!"}}
< {"type":"message.ack","data":{"request_id":"1","message":{…}},"ts":"…"}
< {"type":"message.new","data":{…},"ts":"…"}
```
