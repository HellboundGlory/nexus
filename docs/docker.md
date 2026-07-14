# Running Nexus with Docker

Nexus ships as a single static binary in a minimal
[`distroless/static`](https://github.com/GoogleContainerTools/distroless)
image. Images are published to the GitHub Container Registry for `linux/amd64`
and `linux/arm64`:

```
ghcr.io/hellboundglory/nexus:latest
```

## Quick start (Docker Compose)

1. Grab the example files:

   ```sh
   curl -O https://raw.githubusercontent.com/HellboundGlory/nexus/master/docker-compose.yml
   curl -o .env https://raw.githubusercontent.com/HellboundGlory/nexus/master/.env.example
   ```

2. Edit `.env` — at minimum set `NEXUS_ADMIN_PASSWORD` and (for metadata
   search) `NEXUS_TMDB_API_KEY`.

3. Start it:

   ```sh
   docker compose up -d
   ```

4. Open <http://localhost:9494/> and log in as `admin` with the password you
   set. If you left `NEXUS_ADMIN_PASSWORD` blank, a random password is printed
   once in the logs — find it with:

   ```sh
   docker compose logs nexus | grep "created initial admin user"
   ```

## Quick start (plain `docker run`)

```sh
docker volume create nexus-data
docker run -d --name nexus \
  -p 9494:9494 \
  -e NEXUS_ADMIN_PASSWORD=change-me \
  -e NEXUS_TMDB_API_KEY=your-tmdb-v3-key \
  -v nexus-data:/data \
  ghcr.io/hellboundglory/nexus:latest
```

## Environment variables

| Variable | Default | Notes |
| --- | --- | --- |
| `NEXUS_DATA_DIR` | `/data` | SQLite DB + log file live here. Preset in the image; change only if you remount elsewhere. |
| `NEXUS_HOST` | `0.0.0.0` | Listen address inside the container. Leave as-is; publish with `-p`. |
| `NEXUS_PORT` | `9494` | Listen port. The image's `HEALTHCHECK` reads this too, so they stay in sync. |
| `NEXUS_URL_BASE` | *(empty)* | Reverse-proxy subpath, e.g. `/nexus`. Blank serves at root. |
| `NEXUS_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error`. |
| `NEXUS_API_KEY` | *(random)* | Pin a stable API key; otherwise regenerated each start. **Write-only** — never returned by the API. |
| `NEXUS_TMDB_API_KEY` | *(empty)* | TMDb **v3** API key. Without it, Add-media metadata search returns `not_configured`. **Write-only.** |
| `NEXUS_ADMIN_USER` | `admin` | **First-run only.** Username for the seeded admin account. |
| `NEXUS_ADMIN_PASSWORD` | *(random)* | **First-run only.** Password for the seeded admin. If blank, a random one is logged once at startup. |

**First-run only** means the value is consulted only while the database has no
users. On an existing volume, changing `NEXUS_ADMIN_USER`/`NEXUS_ADMIN_PASSWORD`
has no effect — reset the password from within the app instead, or start against
a fresh volume.

**Write-only** secrets (`NEXUS_API_KEY`, `NEXUS_TMDB_API_KEY`) are accepted as
input but never serialized back in any API response.

## Data & volume permissions

The container runs as the non-root user `nonroot` (uid **65532**) by default, and
the image's `/data` directory is mode **0777** so the SQLite DB stays writable
even if you override the user (see [Downloads & imports](#downloads--imports)).

- **Named volume (recommended):** a fresh named volume inherits the mountpoint's
  ownership+mode, so `/data` is writable immediately — as the default uid or any
  `user:` override. This is what the example Compose file uses.
- **Host bind-mount:** an empty host directory is owned by root. If you run as
  the default nonroot uid, fix it first: `mkdir -p ./data && sudo chown
  65532:65532 ./data`, then mount `./data:/data`.

> **Changing the user later:** an existing named volume keeps the ownership it
> was created with. If you switch to `user: "<PUID>:<PGID>"` after the volume
> already exists and hit DB write errors, recreate it (`docker compose down &&
> docker volume rm <project>_nexus-data && docker compose up -d`) so it re-adopts
> the writable mountpoint.

## Downloads & imports

When a download finishes, Nexus imports **the file at the path your download
client reports** (qBittorrent's `content_path` / SABnzbd's `storage`). Two things
must be true, or you'll see `scan output: … no such file` (the file "doesn't
exist" even though it does):

1. **Same path, both containers.** Mount your media root into the Nexus container
   at the *same path* the download client uses. In a typical linuxserver \*arr
   stack the client mounts `${MEDIA_DIRECTORY}:/media`; give Nexus the identical
   mount:

   ```yaml
   nexus:
     volumes:
       - nexus-data:/data
       - ${MEDIA_DIRECTORY}:/media   # same path the download client sees
   ```

   Then in **Settings → Root Folders** add paths *under that mount* (e.g.
   `/media/movies`, `/media/tvshows`) so imports land where your media server
   reads them.

2. **Write permission.** The other \*arr apps and your library files run as your
   host user (`PUID`/`PGID`). Run Nexus as that same user so it can move imports
   into the library:

   ```yaml
   nexus:
     user: "1000:1000"   # your PUID:PGID
   ```

   (The `/data` volume stays writable under a custom uid — see above.)

Auto-import runs about once a minute, so a completed download imports on its own;
the **Import** button in Activity is only needed to retry. If it still fails,
check that the reported path exists inside the container:
`docker exec` isn't available on the distroless image, so verify the mount from
the client side and confirm the two `:/media` paths match exactly.

## Health

The image defines a `HEALTHCHECK` that runs `nexus healthcheck` — a built-in
subcommand that does a local `GET /health` and exits 0/1. (The distroless base
has no shell or `curl`, so this subcommand is how the healthcheck works.) The
`/health` endpoint is unauthenticated and served at the root regardless of
`NEXUS_URL_BASE`.

Check status with `docker ps` (the `STATUS` column shows `healthy`) or:

```sh
docker inspect --format '{{.State.Health.Status}}' nexus
```

## Reverse proxy on a subpath

Set `NEXUS_URL_BASE=/nexus` and proxy `/nexus/` to the container's port 9494.
The `/health` endpoint stays at the server root (not under the base), which
suits container/orchestrator health probes.

## Upgrades

```sh
docker compose pull
docker compose up -d
```

Data in the `/data` volume persists across upgrades. Pin a specific version
(e.g. `ghcr.io/hellboundglory/nexus:1.2.3`) instead of `:latest` for
reproducible deploys.

## Versioning

The running version is stamped into the binary at build time and shown on the
dashboard / `GET /api/v1/system/status`. Release images are tagged `:latest`
(default branch), `:MAJOR.MINOR.PATCH` and `:MAJOR.MINOR` (git tags `vX.Y.Z`),
and `:sha-<commit>`.
