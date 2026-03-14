# WhatsApp Channel Setup

This project supports WhatsApp in two modes:

- **Bridge mode** (`channels.whatsapp.use_native = false`): connect to an external WebSocket bridge.
- **Native mode** (`channels.whatsapp.use_native = true`): connect directly using `whatsmeow`.

## 1) Does auto-create agent/workspace work for WhatsApp?

Yes.

Auto-provisioning is channel-agnostic and is computed from `(channel, account, peer kind, peer id)`, so WhatsApp direct/group peers can create dedicated auto-provisioned agents the same as other channels.

- Route resolver builds deterministic auto IDs for enabled peer kinds.
- Agent registry lazily creates missing agent instances for auto-provisioned IDs.
- Named/non-main agents get their own workspace directory under the same workspace root (for example, `workspace-auto-...`).

### Minimal config example

```json
{
  "agents": {
    "auto_provision": {
      "enabled": true,
      "chat_types": ["direct", "group"],
      "strict_one_to_one": false
    }
  },
  "channels": {
    "whatsapp": {
      "enabled": true,
      "use_native": true,
      "allow_from": []
    }
  }
}
```

> If `chat_types` is omitted, only `direct` is enabled by default.

## 2) Building WhatsApp native with a container-based setup (Unraid-friendly)

Native mode requires building with Go build tag `whatsapp_native`.

If this tag is missing, startup returns:

`whatsapp native not compiled in; build with -tags whatsapp_native`

### Option A (recommended): build locally from source into your Docker image

Use your local source and tag the local image:

```bash
docker build -t docker.io/sipeed/picoclaw:latest -f docker/Dockerfile .
```

Then run your container from that image.

### Option B: one-off binary build

```bash
go build -tags whatsapp_native ./cmd/...
```

### Important Dockerfile note

`docker/Dockerfile` runs `make build` in the builder stage, and the default build now includes the `whatsapp_native` tag. If you override build tags yourself, make sure `whatsapp_native` remains included.

## 3) Runtime config for native mode

Use:

```json
{
  "channels": {
    "whatsapp": {
      "enabled": true,
      "use_native": true,
      "session_store_path": "/home/picoclaw/.picoclaw/workspace/whatsapp",
      "allow_from": []
    }
  }
}
```

- `session_store_path` should be in your persistent volume.
- With the repository compose file, `./data` is mounted to `/home/picoclaw/.picoclaw`; storing sessions below that path persists WhatsApp login across restarts.

## 4) First-time pairing

On first startup, check container logs and scan the QR with WhatsApp **Linked Devices**.

After successful pairing, logs show the native channel connected.
