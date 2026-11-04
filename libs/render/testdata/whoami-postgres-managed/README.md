# whoami-postgres

This folder runs the stack `whoami-postgres`, designed with Modular Platform. You need Docker with the Compose plugin, nothing else.

## Run it

```sh
sh setup.sh
docker compose up -d --wait --wait-timeout 120
```

`setup.sh` writes the user every module runs as into `.env` and creates the secret files and data folders this stack needs, here on your machine. It never changes a file that already exists, so running it again is safe. If you skip it, `docker compose up` stops with a missing file error instead of starting without passwords.

The first `up` also runs a short job for each database or other resource a module gets made for it; the module starts once its job has finished. Each `up` runs these jobs again, which changes nothing that already exists.

Secret files `setup.sh` creates:

- `secrets/postgres/password`
- `secrets/whoami/uses_postgresql_password`

Data folders `setup.sh` creates; they hold each module's data, so keep them:

- `data/postgres/data`

On Windows, run `setup.sh` in WSL or Git Bash.

## Stop it

```sh
docker compose down
```

## Change it

`graph.yaml` is this stack's graph. Open it in the Modular Platform editor to change the stack, then download it again and start it with `docker compose up -d --wait --remove-orphans`, so a module you removed stops too. Keep your `secrets/` and `data/` folders: they hold the secrets `setup.sh` made and each module's data, and a download never contains them.
