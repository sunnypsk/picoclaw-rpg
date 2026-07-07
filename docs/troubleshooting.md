# Troubleshooting

## "model ... not found in model_list" or OpenRouter "free is not a valid model ID"

**Symptom:** You see either:

- `Error creating provider: model "openrouter/free" not found in model_list`
- OpenRouter returns 400: `"free is not a valid model ID"`

**Cause:** The `model` field in your `model_list` entry is what gets sent to the API. For OpenRouter you must use the **full** model ID, not a shorthand.

- **Wrong:** `"model": "free"` → OpenRouter receives `free` and rejects it.
- **Right:** `"model": "openrouter/free"` → OpenRouter receives `openrouter/free` (auto free-tier routing).

**Fix:** In `~/.picoclaw/config.json` (or your config path):

1. **agents.defaults.model** must match a `model_name` in `model_list` (e.g. `"openrouter-free"`).
2. That entry’s **model** must be a valid OpenRouter model ID, for example:
   - `"openrouter/free"` – auto free-tier
   - `"google/gemini-2.0-flash-exp:free"`
   - `"meta-llama/llama-3.1-8b-instruct:free"`

Example snippet:

```json
{
  "agents": {
    "defaults": {
      "model": "openrouter-free"
    }
  },
  "model_list": [
    {
      "model_name": "openrouter-free",
      "model": "openrouter/free",
      "api_key": "sk-or-v1-YOUR_OPENROUTER_KEY",
      "api_base": "https://openrouter.ai/api/v1"
    }
  ]
}
```

Get your key at [OpenRouter Keys](https://openrouter.ai/keys).

## Image inputs fail with a text-only backbone model

**Symptom:** Text chat works, but messages with image attachments fail with an API error such as `model does not support image input`, `image input is not supported`, or `unsupported image_url`.

**Cause:** The normal backbone model in `agents.defaults.model_name` is used for text turns. If the current turn includes images and `vision_model_name` is configured, PicoClaw routes that turn to the vision model instead. Without a vision route, image media is sent to the selected backbone and text-only models may reject it.

**Fix:** Configure a vision-capable chat model and mark it with `supports_vision: true`:

```json
{
  "agents": {
    "defaults": {
      "model_name": "deepseek",
      "vision_model_name": "gpt4-vision"
    }
  },
  "model_list": [
    {
      "model_name": "deepseek",
      "model": "deepseek/deepseek-chat",
      "api_key": "sk-text"
    },
    {
      "model_name": "gpt4-vision",
      "model": "openai/gpt-5.2",
      "api_key": "sk-vision",
      "api_base": "https://api.openai.com/v1",
      "supports_vision": true
    }
  ]
}
```

Do not use `image_model` for this purpose. `image_model` is reserved for image generation tooling, while `vision_model_name` is for reading uploaded images in chat.
