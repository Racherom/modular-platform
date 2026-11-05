# install

This folder runs the stack `install`, designed with Modular Platform. You need Docker with the Compose plugin, nothing else.

## Run it

```sh
sh setup.sh
docker compose up -d --wait --wait-timeout 120
```

`setup.sh` writes the user every module runs as into `.env` and creates the secret files and data folders this stack needs, here on your machine. It never changes a file that already exists, so running it again is safe. If you skip it, `docker compose up` stops with a missing file error instead of starting without passwords.

`setup.sh` also asks you the values this stack needs. It keeps each answer in `.env` (a secret answer in its file under `secrets/`) and does not ask it again. To answer without a terminal, pipe the answers in, one per line, in the order it asks; a wrong answer then stops it with the name of the value.

It runs each module's setup steps too, in that module's own image, as you and with no network, to make files such as keys and certificates. A step whose files exist is skipped. A step that makes a file for each module bound to its module runs once for every module you bind later, and keeps the file of one you unbind.

The first `up` also runs a short job for each database or other resource a module gets made for it; the module starts once its job has finished. Each `up` runs these jobs again, which changes nothing that already exists.

Secret files `setup.sh` creates:

- `secrets/keeper/master-key`
- `secrets/alpha/uses_vault_client_secret`
- `secrets/beta/uses_vault_client_secret`

Data folders `setup.sh` creates; they hold each module's data, so keep them:

- `data/keeper/data`

On Windows, run `setup.sh` in WSL or Git Bash.

## Stop it

```sh
docker compose down
```

## Change it

`graph.yaml` is this stack's graph. Open it in the Modular Platform editor to change the stack, then download it again and start it with `docker compose up -d --wait --remove-orphans`, so a module you removed stops too. A login made for a module you removed or unbound loses sign in on that `up`; its database and data stay, and binding it again with the same names brings sign in back. Keep your `secrets/` and `data/` folders: they hold the secrets `setup.sh` made and each module's data, and a download never contains them.
