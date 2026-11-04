# sdk-pair

This folder runs the stack `sdk-pair`, designed with Modular Platform. You need Docker with the Compose plugin, nothing else.

## Run it

```sh
sh setup.sh
docker compose up -d --wait --wait-timeout 120
```

`setup.sh` writes the user every module runs as into `.env` and creates the secret files and data folders this stack needs, here on your machine. It never changes a file that already exists, so running it again is safe. If you skip it, `docker compose up` stops with a missing file error instead of starting without passwords.

The first `up` also runs a short job for each database or other resource a module gets made for it; the module starts once its job has finished. Each `up` runs these jobs again, which changes nothing that already exists.

Secret files `setup.sh` creates:

- `secrets/consumer/uses_greet_token`
- `secrets/provider/signing-key`

Settings of the modules built on the module SDK. Fill every line marked `required` before you start, and restart a module after you change its file:

- `config/consumer.env`
- `config/provider.env`

On Windows, run `setup.sh` in WSL or Git Bash.

## Stop it

```sh
docker compose down
```

## Change it

`graph.yaml` is this stack's graph. Open it in the Modular Platform editor to change the stack, then download it again and start it with `docker compose up -d --wait --remove-orphans`, so a module you removed stops too. A login made for a module you removed or unbound loses sign in on that `up`; its database and data stay, and binding it again with the same names brings sign in back. Keep your `secrets/` and `data/` folders: they hold the secrets `setup.sh` made and each module's data, and a download never contains them. Keep your `config/` files too: a download holds them with empty settings, so do not let it replace the ones you filled, and run `setup.sh` again afterwards.
