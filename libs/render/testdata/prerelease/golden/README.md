# prerelease

This folder runs the stack `prerelease`, designed with Modular Platform. You need Docker with the Compose plugin, nothing else.

## Run it

```sh
sh setup.sh
docker compose up -d --wait --wait-timeout 120
```

`setup.sh` writes the user every module runs as into `.env` and creates the secret files and data folders this stack needs, here on your machine. It never changes a file that already exists, so running it again is safe. If you skip it, `docker compose up` stops with a missing file error instead of starting without passwords.

The first `up` also runs a short job for each database or other resource a module gets made for it; the module starts once its job has finished. Each `up` runs these jobs again, which changes nothing that already exists. The service `mp_jobs` is not a module: it only waits for the stack's jobs, so `up --wait` ends once each job has finished, and fails when one fails.

Secret files `setup.sh` creates:

- `secrets/postgres/password`

Data folders `setup.sh` creates; they hold each module's data, so keep them:

- `data/postgres/data`

Settings of the modules built on the module SDK. Fill every line marked `required` before you start, and restart a module after you change its file:

- `config/renderer.env`
- `config/renderer-2.env`

On Windows, run `setup.sh` in WSL or Git Bash.

## Images built on this host

These platform images are not released yet, so no registry holds them. `setup.sh` builds each one first, from Modular Platform at `b5f34e9a1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f`, with the Dockerfile in `build/<module>/`:

- `ghcr.io/racherom/modular-platform/renderer:0.0.0`, from `build/renderer/Dockerfile`

The build needs Docker 23 or newer with BuildKit (`docker buildx`), access to GitHub and Docker Hub, and a few minutes. `docker compose up` then uses these images and pulls none of them.

To build from a checkout of Modular Platform on this machine instead of GitHub (for a commit that is not pushed, or without network to GitHub), point `MP_SOURCE_PATH` at it:

```sh
MP_SOURCE_PATH=path/to/modular-platform sh setup.sh
```

## Stop it

```sh
docker compose down
```

## Change it

`graph.yaml` is this stack's graph. Open it in the Modular Platform editor to change the stack, then download it again and start it with `docker compose up -d --wait --remove-orphans`, so a module you removed stops too. A login made for a module you removed or unbound loses sign in on that `up`; its database and data stay, and binding it again with the same names brings sign in back. Keep your `secrets/` and `data/` folders: they hold the secrets `setup.sh` made and each module's data, and a download never contains them. Keep your `config/` files too: a download holds them with empty settings, so do not let it replace the ones you filled, and run `setup.sh` again afterwards.
