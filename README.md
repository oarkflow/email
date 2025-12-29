# Email Example

This example application accepts a JSON file with SMTP or HTTP email settings, normalizes the fields, expands aliases, and sends the message via the requested transport. The implementation is intentionally flexible so a config only needs the fields that are relevant for the selected provider.

## Placeholder Support

All string fields can reference other values with the `{{placeholder}}` syntax. Common keys include:

- Core metadata: `{{from}}`, `{{from_name}}`, `{{to}}`, `{{cc}}`, `{{bcc}}`, `{{subject}}`, `{{body}}`, `{{text_body}}`, `{{html_body}}`, `{{provider}}`, `{{transport}}`, `{{http_method}}`, `{{endpoint}}`.
- Leftover config values: any extra JSON key becomes available directly (`{{project}}`) and under the `data.` namespace (`{{data.project}}`). Nested values use dot-notation, e.g. `{{release.tag}}`.
- Environment variables: `{{env.MY_SECRET}}` pulls from `os.Getenv("MY_SECRET")`.
- Utility values: `{{now}}` (RFC3339 timestamp), `{{today}}`, `{{timestamp}}`.

Placeholders are evaluated twice: once before defaults (so `from` can reference `username`) and again after defaults (so `subject` can include derived values like `host`). Missing placeholders cause a validation error, making failures obvious during local testing.

## Template Files

In addition to inline strings, you can point any configuration at external files:

- `html_template` (aliases: `template_html`, `html_file`, `html_path`) loads an HTML body from disk.
- `text_template` loads a plain-text body.
- `body_template`, `message_template`, or `msg_template` load a generic message template that feeds the "body" field and flows through the normal HTML/text detection logic.

Template paths are expanded like any other field, so you can keep them in payload overrides or even reference environment placeholders (`"html_template": "{{env.RELEASE_HTML}}"`). Files are read before the final placeholder pass, letting the file contents use `{{project}}`, `{{release.tag}}`, or any other metadata.

This repo includes `templates/release.html` and `templates/release.txt`, both of which are used by `template.smtp.json` to keep rich formatting out of JSON.

## Extensibility

The email sender is designed to be extensible. You can add support for new providers by calling the registration functions:

```go
import "path/to/email/package"

// Add a new SMTP provider
RegisterProviderDefault("myprovider", ProviderSetting{
    Host: "smtp.myprovider.com",
    Port: 587,
    UseTLS: true,
})

// Add an HTTP provider profile
RegisterHTTPProviderProfile("myprovider", httpProviderProfile{
    Endpoint:      "https://api.myprovider.com/v1/send",
    Method:        "POST",
    ContentType:   "application/json",
    PayloadFormat: "myprovider",
})

// Add a custom payload builder
RegisterHTTPPayloadBuilder("myprovider", func(cfg *EmailConfig) (interface{}, string, error) {
    // Custom payload logic here
    return map[string]interface{}{
        "to":      cfg.To,
        "subject": cfg.Subject,
        "body":    cfg.TextBody,
    }, "application/json", nil
})

// Map domains to the provider
RegisterEmailDomainMap("mycompany.com", "myprovider")
```

These functions allow you to extend the system without modifying the core code, enabling support for new email services as they become available.

## Sample Configurations

The folder includes ready-to-run JSON files:

| File | Purpose |
| --- | --- |
| `config.json` | Gmail SMTP example that showcases placeholders across headers and message text. |
| `config.sendgrid.http.json` | SendGrid HTTP API using the built-in payload builder (set `api_key`). |
| `config.mailtrap.http.json` | Mailtrap transactional API example (set `token`). |
| `config.ses.http.json` | AWS SES v2 HTTP API with SigV4 signing (set `aws_access_key`, `aws_secret_key`, `aws_region`). |
| `config.http.custom.json` | Fully custom HTTP payload posted to `https://httpbin.org/post`, useful for dry-runs. |
| `config.mailhog.json` | SMTP example wired to a local MailHog instance on `localhost:1025`. |
| `template.smtp.json` + `payload.release.json` | Demonstrates template/payload split for SMTP releases. |
| `template.http.json` + `payload.http.json` | Demonstrates template/payload split for custom HTTP notifications. |
| `templates/release.html` / `templates/release.txt` | Sample body templates referenced by `template.smtp.json`. |

## Running Examples

From the repo root:

```bash
cd examples/email
# Gmail / SMTP (requires real credentials)
go run . config.json

# SendGrid HTTP (set SG API key first)
export SENDGRID_API_KEY="..."
go run . config.sendgrid.http.json

# Mailtrap HTTP API
go run . config.mailtrap.http.json

# Custom HTTP payload to httpbin (no credentials required)
go run . config.http.custom.json
# Template + payload split
go run . --template template.smtp.json --payload payload.release.json
# or positional shorthand
go run . template.http.json payload.http.json
# Local MailHog test (see section below)
go run . config.mailhog.json
```

> **Tip:** You can keep secrets out of config files by referencing environment placeholders such as `"api_key": "{{env.SENDGRID_API_KEY}}"`.

### Local MailHog Testing

Run MailHog in Docker (or via Homebrew) and point any SMTP config at `localhost:1025` with TLS disabled. The `config.mailhog.json` file already does this so you can validate template rendering without touching production services.

```bash
docker run --rm -p 1025:1025 -p 8025:8025 mailhog/mailhog
cd examples/email
go run . config.mailhog.json
```

Open `http://localhost:8025` to inspect captured messages.

### Demo: run the pipeline workflow against MailHog

You can run the full onboarding pipeline (4-step workflow) using the MailHog-ready example files included in `examples/`.

```bash
# 1) schedule the pipeline workflow (stores jobs in scheduler_store.json)
go run . --schedule --store scheduler_store.json --template examples/pipeline_template_mailhog.json --payload examples/pipeline_payload_mailhog.json

# Quick demo using the simple `template.json`/`payload.json` examples
# schedule a small demo workflow using the lightweight examples/template.json + examples/payload.json
# (also MailHog-ready since template points at localhost:1025 SMTP)
go run . --schedule --store scheduler_store.json --template examples/template.json --payload examples/payload.json

# 2) start a worker that will execute scheduled jobs (in a separate terminal)
go run . --worker --store scheduler_store.json

# 3) visit MailHog UI to see messages: http://localhost:8025
```

To exercise other templates or payload combinations against MailHog, override just the transport fields in your payload JSON (e.g., set `"host": "localhost"`, `"port": 1025`, `"use_tls": false`, and clear credentials). This keeps the body/placeholder coverage identical while routing everything to the local inbox.

### What's New

- HTTP providers now include SES v2 (SigV4), Postmark, SparkPost, Resend, Mailgun form API, alongside existing SendGrid/Brevo/Mailtrap.
- AWS SigV4 signing is automatic when `provider` is `ses`/`aws_ses`/`amazon_ses` or when `http_auth` is set to `aws_sigv4` with AWS credentials and region.
- SMTP auth supports `plain`, `login`, `cram-md5`, or can be disabled with `smtp_auth: none`.
- Inline attachments are supported; set `"inline": true` and optional `"content_id"` per attachment to embed images into HTML bodies.
- Delivery headers: `return_path`, `list_unsubscribe`, `list_unsubscribe_post`, SES `configuration_set`, and `tags` are now configurable.

## Scheduling & Workflows 🔧

This release introduces a lightweight scheduler and workflow helper built into the binary.

- Start a long-running worker that polls a job store and sends scheduled emails:

```bash
# start a worker (persisted store at scheduler_store.json by default)
go run . --worker
```

- Schedule an email (or a pre-defined workflow) instead of sending immediately:

```bash
# schedule a single email described by the payload/template
go run . --schedule template.json payload.json

# schedule the welcome workflow if `"workflow":"welcome"` is set in the payload
go run . --schedule template.json examples/workflow_payload.json
```

> Note: workflows are **not** scheduled automatically unless you pass `--schedule` (so running without `--schedule` will attempt an immediate send).
- The job store is a simple JSON file (`scheduler_store.json`) by default and is suitable for single-process execution; a pluggable store interface is provided to add DB-backed persistence later.

### Workflow schema (custom)

You can define arbitrary workflows by adding a `workflow_steps` array to your payload. Each step is an object with optional overrides for that step. Example step fields:

- `name` (string): a human name for the step
- `delay_seconds` (number): schedule the step `delay_seconds` after scheduling time
- `run_at` (RFC3339 string): schedule at an exact time instead of using `delay_seconds`
- `subject`, `body`, `html_body`: message overrides for the step
- `to`, `cc`, `bcc`: recipient overrides
- `provider_priority`: array of provider names to attempt for that step, in order
- `retry_count`, `retry_delay_seconds`, `max_retry_delay_seconds`: retry/backoff settings for the step

Example usage:

```bash
# schedule a custom workflow from a payload file
# 1) schedule the example pipeline (uses `examples/pipeline_template.json` + `examples/pipeline_payload.json`)
go run . --schedule --store scheduler_store.json --template examples/pipeline_template.json --payload examples/pipeline_payload.json

# 2) start a worker that will execute scheduled jobs (persisted to scheduler_store.json)
go run . --worker --store scheduler_store.json

# 3) inspect scheduled jobs by viewing `scheduler_store.json` (it is plain JSON)
cat scheduler_store.json | jq .
```

For the example files added above:

- `examples/pipeline_template.json` is the base template (contains `html_template` / `text_template` pointing to files in `templates/`).
- `examples/pipeline_payload.json` contains `workflow_steps` with four onboarding steps (welcome, credentials, walkthrough, idle_reminder) demonstrating delays, per-step retry and provider priority overrides.

Notes:

- Step overrides may set `subject`, `body`, `html_body`, `to`, `provider_priority`, and retry/backoff settings (`retry_count`, `retry_delay_seconds`, `max_retry_delay_seconds`).
- When scheduling large numbers of jobs or for production use, swap `FileJobStore` for a durable DB-backed store (pluggable `JobStore` interface).

## Custom Payloads

When `type` is set to `http`, the sender can:

- Rely on smart provider defaults (SendGrid, Brevo/Sendinblue, Mailtrap).
- Supply `http_payload` to fully control the JSON body. All placeholders are expanded recursively, so payload snippets can safely reference `{{subject}}`, `{{to}}`, or any custom metadata.
- Provide `headers` and `query_params` maps that also accept placeholders.

Attachments, reply-to lists, and even file paths can use the same placeholder syntax, making it easy to describe templated notifications without touching Go code.

---

## Provider Routing & Selection 🔀

You can define **routes** to conditionally select providers based on the message. Add a top-level `routes` key (array) where each route may include any of:

- `to_domain` / `to_domains`: match recipient domains (e.g. `"gmail.com"`).
- `from_domain` / `from_domains`: match sender domain.
- `subject_regex`: a simple regex string to match the subject.
- `provider_priority`: ordered list of providers to try for matched messages.
- `provider`: single-provider shortcut when only one is desired.
- Rate limits: `hourly_limit`, `daily_limit`, `weekly_limit`, `monthly_limit` to avoid overusing a provider.
- `selection_window`: a duration string (e.g. `"1h"`, `"24h"`) that controls the lookback window used for usage-based provider selection. Defaults to `24h`.
- `provider_weights`: an object mapping provider names to numeric weights; higher weight penalizes selection (e.g. `{ "sendgrid": 1.5, "smtp": 1.0 }`).

Behavior:

- Global `provider_priority` (on the message) takes precedence.
- Otherwise, the first matching route whose limits are not exhausted is chosen.
- If a route lists multiple providers, the system orders them by a score computed from recent usage counts within the `selection_window` and provider weights (score = count * weight). The provider with the lowest score is preferred.
- When `dry_run` is set true in the config, no actual send is performed; the program logs which providers *would* have been used.

Example route snippet:

```json
"routes": [
  {
    "to_domain": "gmail.com",
    "provider_priority": ["sendgrid", "smtp"],
    "selection_window": "1h",
    "recency_half_life": "1h",
    "provider_weights": {"sendgrid": 1.2, "smtp": 1.0},
    "provider_capacities": {"sendgrid": 100, "smtp": 1000},
    "provider_costs": {"sendgrid": 1.2, "smtp": 1.0},
    "hourly_limit": 200
  }
]
```

This enables cost-aware and usage-aware routing while preventing accidental overuse of a provider.

---

For local testing with MailHog, use the included `examples/routes_example.json` and set `dry_run` to `false` if you want actual delivery to the local SMTP server. Start MailHog with Docker:

```bash
docker run --rm -p 1025:1025 -p 8025:8025 mailhog/mailhog
# then run the example
go run . examples/routes_example.json
```
