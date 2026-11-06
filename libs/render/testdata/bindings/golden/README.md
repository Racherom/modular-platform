# bindings

This folder runs the stack `bindings`, designed with Modular Platform. You need Docker with the Compose plugin, nothing else.

## Run it

```sh
sh setup.sh
docker compose up -d --wait --wait-timeout 120
```

`setup.sh` writes the user every module runs as into `.env` and creates the secret files and data folders this stack needs, here on your machine. It never changes a file that already exists, so running it again is safe. If you skip it, `docker compose up` stops with a missing file error instead of starting without passwords. The settings of each module are in `.env`; after you edit `.env`, run `./setup.sh` again, which checks every value there, then `docker compose up -d`.

The first `up` also runs a short job for each database or other resource a module gets made for it; the module starts once its job has finished. Each `up` runs these jobs again, which changes nothing that already exists. The service `mp_jobs` is not a module: it only waits for the stack's jobs, so `up --wait` ends once each job has finished, and fails when one fails.

Secret files `setup.sh` creates:

- `secrets/app/notify-key`
- `secrets/app/uses_audit_password`
- `secrets/app/uses_notify_token`

Files you place yourself, from the service outside this stack they belong to (`setup.sh` stops and lists every one that is missing):

- `secrets/billing/db_password`: password of billing. Password of the login.
- `secrets/shared/admin_admin_password`: admin_password of shared. Superuser password.

On Windows, run `setup.sh` in WSL or Git Bash.

## Stop it

```sh
docker compose down
```

## Change it

`graph.yaml` is this stack's graph. Open it in the Modular Platform editor to change the stack, then download it again and start it with `docker compose up -d --wait --remove-orphans`, so a module you removed stops too. A login made for a module you removed or unbound loses sign in on that `up`; its database and data stay, and binding it again with the same names brings sign in back. Keep your `secrets/` and `data/` folders: they hold the secrets `setup.sh` made and each module's data, and a download never contains them.
