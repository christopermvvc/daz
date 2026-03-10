# ollama plugin

Generate chat style responses using an Ollama endpoint.

Config (optional)
- `enabled` (default: true)
- `ollama_url` (default: `http://localhost:11434`)
- `model` (default: `gemma3:1b`)
- `rate_limit_seconds` (default: 5)
- `temperature` (default: 0.7)
- `max_tokens` (default: 2048)
- `keep_alive` (default: `5m`)
- `system_prompt` (default: hardened Dazza persona and safety guardrails)
- `follow_up_enabled` (default: true)
- `follow_up_window_seconds` (default: 180)
- Max enforced: values above `900` are clamped to 900 seconds (15 minutes).
- `follow_up_respond_all_messages` (default: false)
- `follow_up_max_messages` (default: 4)
- `follow_up_max_messages_jitter` (default: 1)
- `follow_up_min_interval_ms` (default: 2500)
- `follow_up_noise_chance` (default: 0.08)
- `follow_up_no_response_chance` (default: 0.05)

Notes
- `follow_up_noise_chance` adds short human-like interjections after the second follow-up turn.
- `follow_up_no_response_chance` allows occasional skips to reduce repetitive responses.
- Set either field to `0` to disable that specific behavior.
- Values greater than `1` are clamped to `1` in runtime validation.
- The generated replies include an explicit boundary that chat history is context-only.

Invocation handling
- The plugin accepts bot invocations anywhere in the message, not just at the start (`hey @Dazza, ...`, `yo daz ...`).
- Manual invocation tokens are removed from the user message before passing it to the model.
