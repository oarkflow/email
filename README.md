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

Open `http://localhost:8025` to inspect captured messages. To exercise other templates or payload combinations against MailHog, override just the transport fields in your payload JSON (e.g., set `"host": "localhost"`, `"port": 1025`, `"use_tls": false`, and clear credentials). This keeps the body/placeholder coverage identical while routing everything to the local inbox.

### What's New

- HTTP providers now include SES v2 (SigV4), Postmark, SparkPost, Resend, Mailgun form API, alongside existing SendGrid/Brevo/Mailtrap.
- AWS SigV4 signing is automatic when `provider` is `ses`/`aws_ses`/`amazon_ses` or when `http_auth` is set to `aws_sigv4` with AWS credentials and region.
- SMTP auth supports `plain`, `login`, `cram-md5`, or can be disabled with `smtp_auth: none`.
- Inline attachments are supported; set `"inline": true` and optional `"content_id"` per attachment to embed images into HTML bodies.
- Delivery headers: `return_path`, `list_unsubscribe`, `list_unsubscribe_post`, SES `configuration_set`, and `tags` are now configurable.

## Custom Payloads

When `type` is set to `http`, the sender can:

- Rely on smart provider defaults (SendGrid, Brevo/Sendinblue, Mailtrap).
- Supply `http_payload` to fully control the JSON body. All placeholders are expanded recursively, so payload snippets can safely reference `{{subject}}`, `{{to}}`, or any custom metadata.
- Provide `headers` and `query_params` maps that also accept placeholders.

Attachments, reply-to lists, and even file paths can use the same placeholder syntax, making it easy to describe templated notifications without touching Go code.
