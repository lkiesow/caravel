# Upgrading

Pull the new image and restart. Migrations run at startup, so there is no
separate step:

```sh
docker compose pull
docker compose up -d
```

From a source checkout, `git pull` and `make run`.

## Confirming which build is running

`/api/health` reports the version stamped into the binary at build time, so the
answer comes from the running process rather than from the tag you believe you
deployed:

```sh
curl http://localhost:8080/api/health
```

```json
{"status":"ok","version":"v1.0.0"}
```

The container's healthcheck runs the same check internally (`caravel -health`).
The image is distroless and has no shell, so `docker logs` rather than
`docker exec` is how you see what a container is doing.

## Migrations

They run automatically, on both dialects, when the server starts. A migration
that fails stops startup rather than leaving a half-migrated database serving
requests.

Because of that, the safe order for an upgrade is the ordinary one — stop, pull,
start — and the useful precaution is a [backup](backup.md) taken immediately
before, since a schema change is the one upgrade step that is not simply a
matter of putting the old image back.

## Downgrading

Putting an older image back works only if the newer one did not migrate the
schema. If it did, the old binary will not recognise the newer schema version
and will refuse to start. Restoring the pre-upgrade backup is the route back.

!!! warning "Databases created before the first release"

    The migration history was squashed to a single initial migration before any
    image was published. A database created by a pre-release checkout of this
    repository cannot be migrated forward — it fails at startup with `no
    migration found for version 12` — and has to be recreated. If you installed
    from a published image, this does not apply to you.
