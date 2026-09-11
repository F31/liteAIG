# Webhook Notifications

Enable Stage 14 notifications with `--webhook-url=https://receiver.example/events`
alongside `--db`. App embedders can set `LiteOptions.WebhookURL`. An empty URL
disables webhook delivery without changing the analytics sink.

The Admin API also persists the tenant notification target for operator-managed
configuration:

- `GET /api/admin/alerts/notifications`
- `PUT /api/admin/alerts/notifications` with `{"webhookUrl":"https://receiver.example/events","enabled":true}`
- `DELETE /api/admin/alerts/notifications`

These endpoints hot-reload the current process after persisting the desired
state. On startup, an explicit `--webhook-url`/`LiteOptions.WebhookURL` wins;
otherwise Lite loads the persisted tenant setting when it is enabled. Webhook
URLs must be HTTP(S), pass the Lite egress policy, and must not contain URL
userinfo.

The receiver gets JSON POSTs with `id`, `kind`, `occurred_at`, `tenant_id`, optional
`project_id` and `request_id`, and an `attributes` object. IDs identify the source
event or lifecycle resource; an alert or approval ID may recur across transitions.
Kinds currently delivered:

- `alert.firing`, `alert.ack`, `alert.silence`, `alert.resolve`
- `approval.created`, `approval.partial`, `approval.approved`, `approval.rejected`
- `guardrail.match`, `guardrail.retroactive` (including streaming guardrails)

Attributes are allowlisted metadata: `rule_id`, `severity`, `status`, `action`,
`content_hash`, when supplied by the emitter. Prompt/response bodies, alert
messages/evidence, approval requester/target details, arbitrary attributes, and
secrets are not included. Request-completion and other analytics events are not
sent to the webhook. Existing analytics delivery remains independent.

Targets use `egress.ValidateTarget` and the guarded `egress.Client` with the
existing Lite policy: HTTP(S), loopback allowed for local operator endpoints,
private/link-local/metadata destinations blocked, dial-time hostname checks,
redirects rejected, and response bodies capped. Use HTTPS outside local testing.

Delivery is best effort, not a durable outbox: one background worker, a 128-event
queue, five seconds per HTTP request, no retries or signing. Non-2xx responses
are failures. A full queue drops notifications; failed deliveries and queue drops
produce generic logs without the URL or response body. Neither changes gateway
responses or approval/alert persistence. `Lite.Close` drains the queue for up to
five seconds, then cancels outstanding delivery and closes idle connections.
Pending notifications may be lost on overload, shutdown timeout, or process exit.

This adds notifications to existing lifecycle operations; it does not introduce
an alert evaluation scheduler. Split deployments must enable the URL on the
processes that produce the desired control-plane and gateway events.
