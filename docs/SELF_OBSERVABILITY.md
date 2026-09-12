# Self-Observability: Claude Code Telemetry in Honeycomb

Claude Code exports its own OpenTelemetry metrics, log events, and (in beta) spans. This
document records how that export is configured for this project, what the data actually
looks like once it lands in Honeycomb, and which parts of the vendor documentation do not
match the wire format.

Prompted by Honeycomb's [Can Claude Code observe its own code?][post]; the reference for
the variables themselves is the [Claude Code monitoring documentation][docs].

[post]: https://www.honeycomb.io/blog/can-claude-code-observe-its-own-code
[docs]: https://code.claude.com/docs/en/monitoring-usage

Board: **Claude Code Monitoring** — `gracefulcode` / `test` environment, board `eJUWrCnEqW7`.
Panel specs are in [`claude-code-board.json`](./claude-code-board.json).

## Configuration

See [`../scripts/claude-code-telemetry.env.example`](../scripts/claude-code-telemetry.env.example).
Copy it somewhere your shell sources and `source` it from `~/.zshrc` *after* the line that
sets `HONEYCOMB_API_KEY`. The file references the key and never inlines it.

| Variable | Value | Why |
| --- | --- | --- |
| `CLAUDE_CODE_ENABLE_TELEMETRY` | `1` | Master switch. |
| `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` | `1` | Emits spans, not just metrics and log events. Beta; shape may change. |
| `OTEL_METRICS_EXPORTER` / `OTEL_LOGS_EXPORTER` / `OTEL_TRACES_EXPORTER` | `otlp` | One per signal; each is independent. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | Fewer transport failure modes on macOS than the `grpc` + `api.honeycomb.io:443` form. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `https://api.honeycomb.io` | US region. Use the EU host if the team is EU-resident. |
| `OTEL_EXPORTER_OTLP_HEADERS` | `x-honeycomb-team=${HONEYCOMB_API_KEY}` | See *Dataset routing* below for why there is no dataset header. |
| `OTEL_LOG_USER_PROMPTS` | `1` | Prompt text. Without it, only `prompt_length`. |
| `OTEL_LOG_ASSISTANT_RESPONSES` | `1` | Response text. |
| `OTEL_LOG_TOOL_DETAILS` / `OTEL_LOG_TOOL_CONTENT` | `1` | Tool parameters and tool input/output. |
| `OTEL_METRICS_INCLUDE_VERSION` / `..._ENTRYPOINT` | `true` | Adds `app.version` and `app.entrypoint`; off by default. |

The API key needs `events: true` and `createDatasets: true`. A Honeycomb **configuration**
key with those scopes works for ingest — it does not have to be an ingest key. Check with:

```sh
curl -s https://api.honeycomb.io/1/auth -H "X-Honeycomb-Team: $HONEYCOMB_API_KEY"
```

`~/.zshrc` only affects new shells, so an already-running Claude Code session will not
export anything until it is restarted.

## Dataset routing

Verified empirically on 2026-09-12, and this is the single most surprising part:

| Signal | Lands in | Identified by |
| --- | --- | --- |
| Logs (events) | `claude-code` | `service.name` |
| Traces (spans) | `claude-code` | `service.name` |
| Metrics | `metrics` (shared with every other producer) | `service.name = claude-code` |

**The `x-honeycomb-dataset` header is ignored for every signal** — on the generic
`OTEL_EXPORTER_OTLP_HEADERS` and on `OTEL_EXPORTER_OTLP_METRICS_HEADERS` alike. Setting it
to `claude-code-metrics` does not create that dataset; metrics still arrive in `metrics`.
The header is therefore omitted, and every metric query filters `service.name = claude-code`
instead. (The blog post's `x-honeycomb-dataset=claude_metrics` does not reproduce here.)

Logs and spans share the `claude-code` dataset. Separate them with `meta.signal_type`,
which is `log` or `trace`.

## What the data actually looks like

### Log events — `claude-code`, `meta.signal_type = log`

Identified by `event.name`. **The values are bare, not `claude_code.`-prefixed** as the
documentation's event table implies:

`api_request`, `assistant_response`, `user_prompt`, `tool_result`, `tool_decision`,
`hook_registered`, `hook_execution_start`, `hook_execution_complete`,
`mcp_server_connection`, `plugin_loaded`

The last five are undocumented. `api_error`, `api_refusal`, and `permission_check` are
documented and appear once those conditions occur.

Useful columns, all confirmed populated:

| Column | Type | On |
| --- | --- | --- |
| `cost_usd`, `cost_usd_micros` | float / int | `api_request` |
| `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens` | int | `api_request` |
| `duration_ms` | string (aggregates numerically) | `api_request`, `tool_result` |
| `ttft_ms`, `first_content_ms` | int | `api_request` |
| `model`, `request_id`, `stop_reason` | string | `api_request` |
| `tool_name`, `success`, `tool_use_id` | string / bool / string | `tool_result` |
| `decision`, `source` | string | `tool_decision` |
| `prompt`, `prompt_length` | string | `user_prompt` |
| `session.id`, `user.email`, `organization.id`, `terminal.type` | string | all |

Two traps:

- **`prompt_length`, not `user_prompt_length`.** Both columns exist; only `prompt_length`
  is ever written. A panel built on `user_prompt_length` renders a flat zero and looks
  like working instrumentation.
- **`duration_ms` is typed `string`** because the log events write it as a string
  attribute before any span does. Honeycomb still coerces it for `P50`/`P95`/`P99`, so
  percentile calculations work — but it is not the numeric column it appears to be.

### Spans — `claude-code`, `meta.signal_type = trace`

Identified by `name`. This is what `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` adds:

| Span | Role |
| --- | --- |
| `claude_code.interaction` | Trace root — one per user turn. |
| `claude_code.llm_request` | One model call. |
| `gen_ai.request.attempt` | Zero-duration marker per attempt. |
| `claude_code.tool` | A tool call, wrapping the two below. |
| `claude_code.tool.blocked_on_user` | Time waiting on a permission prompt, isolated from real work. |
| `claude_code.tool.execution` | The tool actually running. |
| `tool.output` | Zero-duration marker. |

`gen_ai.request.attempt` and `tool.output` always have `duration_ms = 0`; they are markers,
not work. Exclude them from latency panels.

### Metrics — `metrics`, `service.name = claude-code`

Only six metric columns exist so far:

`claude_code.token.usage`, `claude_code.cost.usage`, `claude_code.session.count`,
`claude_code.active_time.total`, `claude_code.lines_of_code.count`,
`claude_code.code_edit_tool.decision`

Two traps:

- **A column that has never been written is a hard query error**, not an empty series.
  `claude_code.commit.count` and `claude_code.pull_request.count` do not exist until Claude
  Code creates a commit or a PR, and a panel referencing one fails the whole query. Add
  those panels only after the metric first appears.
- **One metric per panel.** Combining metrics whose datapoints do not overlap returns
  `query execution returned no results` for the entire query — not a partial result. Two
  co-occurring metrics (`token.usage` + `cost.usage`) do work, but the safe rule is one.

The `type` attribute is shared across metrics: it carries `input`/`output`/`cacheRead`/
`cacheCreation` for tokens and `added`/`removed` for lines of code. Breaking down
`lines_of_code.count` by `type` without filtering surfaces zero-valued token rows, so the
lines-of-code panel filters `type IN [added, removed]`.

## Privacy

All four content flags are on, so **every Claude Code session on this machine ships prompt
text, assistant response text, tool parameters, and tool input/output to Honeycomb** — file
contents read by `Read`, diffs written by `Edit`, and stdout captured by `Bash` included.
`user.email` is attached to every event.

Anything typed into or read by Claude Code in any repo is queryable by anyone with access
to the `gracefulcode` team. To narrow this, drop `OTEL_LOG_TOOL_CONTENT` (the broadest) and
then `OTEL_LOG_ASSISTANT_RESPONSES`; metrics, timings, tool names, and success flags all
survive with every content flag off.

Cardinality: `session.id`, `prompt.id`, `message.uuid`, and `client_request_id` are
unbounded. Fine at single-user volume; reconsider before enabling this fleet-wide.

## Verifying

```sh
# 1. Key has ingest + dataset creation
curl -s https://api.honeycomb.io/1/auth -H "X-Honeycomb-Team: $HONEYCOMB_API_KEY"

# 2. New shell, then confirm the exporters are live
env | grep -c '^OTEL_'          # expect 12

# 3. Generate telemetry without an interactive session
claude -p "Use Bash to run 'echo hello'." --allowedTools Bash
```

Log events flush every 5s (`OTEL_LOGS_EXPORT_INTERVAL`), metrics every 60s
(`OTEL_METRIC_EXPORT_INTERVAL`). Then confirm in Honeycomb that the `claude-code` dataset
has recent events and that `metrics` has `claude_code.*` columns with
`service.name = claude-code`.

Note that the `/1/datasets` API's `last_written_at` lags by minutes; query the dataset
directly rather than trusting it to decide whether ingest is working.
