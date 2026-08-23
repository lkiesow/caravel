# Backup and restore

Caravel's data is in two places, and a backup of one without the other is not a
backup.

| What | Default location | In the container |
|---|---|---|
| The database | `data/caravel.db` (SQLite) | `/data` |
| Uploaded photos and documents | `uploads/` | `/uploads` |

Uploads are files on disk, not blobs in the database. The database references
them by path, and the files are not useful without the metadata pointing at
them — so restoring one without the other gives you either a gallery of
orphans or a trip full of broken images.

!!! danger "Back them up in one operation"

    Not two jobs on two schedules. A database from Tuesday and an upload
    directory from Sunday is a restore that half works, and you find out which
    half only afterwards.

## SQLite

The whole database is one file, so a backup is a copy:

```sh
tar czf caravel-backup.tar.gz data/caravel.db uploads/
```

Stopping the app first guarantees a clean snapshot. Copying it live is safe in
practice — SQLite runs in WAL mode — but if you can accept a few seconds of
downtime, stopping is the version with nothing to reason about.

With compose the data is in two named volumes, so the archive is made from
inside a throwaway container. Note the volume names are prefixed with the compose
project — the directory name — so check them first:

```sh
docker volume ls | grep caravel
# caravel_caravel-data
# caravel_caravel-uploads
```

Then, with the app stopped so SQLite has checkpointed:

```sh
docker compose stop caravel
docker run --rm \
  -v caravel_caravel-data:/data \
  -v caravel_caravel-uploads:/uploads \
  alpine tar czf - /data /uploads > caravel-backup.tar.gz
docker compose start caravel
```

The archive goes to **stdout** and is redirected by your own shell rather than
written through a second bind mount. That is deliberate: mounting a host
directory into the container to write the file there works on Docker but fails
under rootless Podman, where the container's user cannot write to your
directory. Redirecting works identically on both.

Restoring is the same operation reversed, into a stopped instance:

```sh
docker compose stop caravel
docker run --rm -i \
  -v caravel_caravel-data:/data \
  -v caravel_caravel-uploads:/uploads \
  alpine tar xzf - -C / < caravel-backup.tar.gz
docker compose start caravel
```

## Postgres

Use normal Postgres tooling for the database — `pg_dump`/`pg_restore`, or your
provider's snapshot mechanism — and back up the upload directory alongside it,
in the same job:

```sh
docker compose -f docker-compose.postgres.yml exec db \
  pg_dump -U caravel caravel > caravel.sql
```

The pairing rule is unchanged: the dump and the uploads have to come from the
same moment.

## Test the restore

A backup nobody has restored is a hypothesis. Restoring into a throwaway
instance — a second compose project on a different port, pointed at a copy of
the archive — is a cheap way to find out whether it works before you need it to.

## What is not backed up here

**Sessions** live in the database, so a restore keeps everyone logged in. That is
usually what you want. If it is not, note that only a user changing their *own*
password logs their devices out — an admin resetting someone's password
deliberately leaves their sessions alone.

**Nothing else has state.** The rate limiters are in memory and reset on restart,
and the frontend is embedded in the binary. There is no cache to warm and no
search index to rebuild.
