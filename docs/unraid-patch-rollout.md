# Unraid patch rollout

This runbook applies to `picoclaw-rpg`. Local validation does not authorize a
commit, publication, NAS downtime, data migration, or a real WhatsApp message.
Perform those stages only after their separate approval. Never run two gateways
against the same WhatsApp database.

## Read-only inventory

On Unraid:

```sh
PICO_CONTAINER=picoclaw-rpg
docker inspect --format 'ImageID={{.Image}} Image={{.Config.Image}} Started={{.State.StartedAt}}' "$PICO_CONTAINER"
docker inspect --format '{{range .Mounts}}{{println .Type .Source "->" .Destination}}{{end}}' "$PICO_CONTAINER"
docker inspect --format 'User={{.Config.User}} Network={{.HostConfig.NetworkMode}} Entrypoint={{json .Config.Entrypoint}} Cmd={{json .Config.Cmd}}' "$PICO_CONTAINER"
docker exec "$PICO_CONTAINER" /usr/local/bin/picoclaw version
docker image inspect --format 'Architecture={{.Architecture}} Digests={{json .RepoDigests}}' "$(docker inspect --format '{{.Image}}' "$PICO_CONTAINER")"
```

Keep the full container configuration privately for rollback; it may contain
credentials. Do not paste environment variables or the full configuration into
logs or a public issue. Resolve the actual persistent host directory from the
mount listing. Do not assume `docker/data` or merge agent workspaces.

## Build and release

Run local tests, native WhatsApp tests, Linux race tests, vet, dependency
verification and the standard image build before publication. The GHCR workflow
uses a full `sha-<commit>` tag and embeds that SHA, build time, and Go version.
After an authorized commit/push, require a successful workflow including both
architecture metadata checks. Record its manifest digest and pin the Unraid image
to `ghcr.io/<owner>/<repo>@sha256:<verified-manifest-digest>`.

An image built from an uncommitted local patch is only a validation artifact;
its baseline SHA is not a publishable identity for the patch. Do not deploy it.

## Stop and cold backup (separate approval)

1. Retain the current image ID and an exported recoverable image or immutable
   digest. Disable automatic container updates during the cutover.
2. Stop the old gateway and verify it is stopped before copying or modifying data.
3. Back up the **entire** persistent mount with ownership and permissions intact,
   including configuration, all workspaces, cron files, `private-media/images`, WhatsApp database, WAL
   and SHM files. Store backups outside the mounted directory. Confirm the backup
   can be read and restored. Do not copy only `store.db` from a running gateway.
4. Rehearse the repair against a separate copy of the backup. Never start a second
   authenticated gateway against that copy for testing.

## Repair the selected job

Use the new image with `--entrypoint /usr/local/bin/picoclaw`, the original
mount destinations and user permissions, and no gateway command. This avoids the
normal entrypoint's workspace synchronization. Mount the rehearsal copy first.
If a custom config location is used, retain its original config selection.

The following arguments are passed to that one-shot binary:

```sh
cron repair --id 9b31a8aa031e4762 --timezone Asia/Hong_Kong --min-verified-sources 2
```

The default is read-only. Review the old/new route and the returned file SHA-256.
No task content is printed. The new route must identify the destination group
using the configured bindings. Then, with the gateway still stopped:

```sh
cron repair --id 9b31a8aa031e4762 --timezone Asia/Hong_Kong --min-verified-sources 2 \
  --apply --expected-hash <hash-from-preview> --gateway-stopped
```

Apply creates `jobs.json.backup-<original-sha256>` and writes atomically. Hash
drift stops the operation. Keep this backup with the full cold backup. Confirm
ID, message, enabled state, `deliver:false`, and `15 9 * * *` remain unchanged.
Only this job receives the policy/timezone change. A preview without `--id`
reports other routes; do not apply repairs to unrelated jobs automatically.
The timezone is used by the scheduler, not merely displayed.

Repeat against the production mount only after the rehearsal and separate
approval. A gateway that is still running can overwrite offline repairs; the
`--gateway-stopped` flag is an operator assertion, not remote process detection.

## Start and acceptance (separate approval)

- Start one gateway using the verified digest, original mounts, user, network
  and settings. Preserve both agent workspaces separately.
- Read `picoclaw version`, image OCI revision, architecture and actual digest.
  Check the next run is 09:15 `Asia/Hong_Kong` (01:15 UTC).
- Send a genuine JPEG/PNG/WebP and edit it. Ask again after intervening messages,
  restart, and use its explicit reference. Retention ends three hours after
  receipt; restarting does not renew it. `media://current` means this turn only.
- Verify different senders/groups cannot reuse its ref; ambiguous candidates
  require clarification. The old animation ZIP cannot recover the original
  photograph: ask the sender to upload the photograph again.
- Observe the next authorized 09:15 news run. It must produce one final result.
  Failed verification must say:
  `今朝未能完成新聞核實，暫時無法提供可靠摘要。`
  and mark the job failed. No later delivery is promised.
- Correlate `trace_id`, queue logging and `WhatsApp API accepted` with its platform
  `message_id`. API acceptance does not prove delivery or reading. Prepared and
  queued messages are not platform acceptance.

## Rollback

Stop the new gateway first. Preserve a separate snapshot of all post-upgrade
data before restoring anything. Restore the old image; if its WhatsApp database
format is incompatible, restore the matching complete cold backup including WAL
state and configuration. Keep the post-upgrade snapshot so newly created data
is not silently lost. Recheck login, routing and version before normal operation.

## News result contract

Jobs with `min_verified_sources:0` retain ordinary behavior. Constrained news
runs use `web_fetch` evidence from that run and return JSON with `status` equal
to `news`, `no_news`, or `verification_failed`. Each `news` item has `text` and
`source_urls`; `no_news` uses top-level `text` and `source_urls`. The gate counts
independent sites (registrable domains), including final redirect destinations,
not URLs from the same site or search snippets. It checks successful reading and
source count; the model must still check dates, facts and source reliability.

## Shell protection prerequisite

With private image persistence enabled, shell subprocesses require Linux
Landlock ABI 3 or later. A separate restricted OS thread starts each child with
filesystem access excluding the private image directory and its ancestor
permissions. Normal workspace file operations remain available; listing or
creating files directly in a protected directory's ancestors may be denied.
Shell expansion (`~`, environment variables, constructed paths) cannot grant
access to the protected image bytes. Ordinary file tools independently deny the
private directory. See the [Linux Landlock documentation](https://docs.kernel.org/userspace-api/landlock.html).

If the kernel/container blocks Landlock, or on non-Linux hosts, `exec` fails
closed while private media protection is enabled. The gateway and scoped image
tools continue operating. Confirm this capability during the NAS rehearsal;
do not disable the protection to make an incompatible host pass acceptance.
