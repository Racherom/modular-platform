# Modular Platform, managed mode

This folder runs the Modular Platform on your own host. Once it runs, you sign in as the admin, design a stack in the editor, deploy it to this host, and watch each module's state live.

You need a Linux host with Docker Engine and the Docker Compose plugin. Nothing else.

Every file here is rendered from `graph.yaml`, the platform's own graph: each platform service is a catalog module, bound to the others like the modules of any stack.

## Install

1. Unzip `modular-platform-<version>.zip` where the platform should live, for example `/opt/modular-platform`, and open that folder.
2. Run `sh setup.sh`. It asks two things, in this order:
   - the domain the platform answers on (it must contain a dot, like `platform.localhost`),
   - the origin your browser sees (press Enter for `https://<domain>`).

   It writes `.env`, creates every secret in `secrets/` and every data folder in `data/`, and prints the setup code once. Write it down, or read it later from `secrets/api/setup/code`; the file stays until the setup is done, so read the file and not your terminal scrollback. Apart from the setup code, it never prints a secret. Running it again changes no file that exists and asks nothing it asked before. To answer without a terminal, pipe the answers in, one per line, in the order above.
3. Run `docker compose up -d --wait`.
4. Open the origin, enter the setup code, and make the admin account (see *Sign in*).

The platform makes its own certificate authority for HTTPS (`MP_INGRESS__TLS_MODE=internal`), so your browser warns about the certificate the first time. For a public certificate, set `MP_INGRESS__TLS_MODE=acme` in `.env` (the domain must be public), run `sh setup.sh` again, which checks every value, then `docker compose up -d`.

The settings of the ingress, the editor and the identity provider are in `.env`; [`.env.example`](.env.example) lists each one with its default and what it does. The settings of `api`, `renderer` and `deployer-docker` are in `config/<instance>.env`. Never put a secret in either: secrets live in `secrets/`.

## Sign in

The bundled identity provider is Pocket ID. It signs people in with passkeys only, so there is no password to type or to lose. You need a device that can make a passkey (a phone, a security key, or a browser or password manager that stores them).

The first visit makes the admin account:

1. Open the origin. The page asks for the setup code that `setup.sh` printed. A code works once, and more than ten tries in a minute from one address are refused.
2. Choose *Create your admin account*. It opens Pocket ID's own page on the auth host. Make the account there and register a passkey. Pocket ID has no way back to the platform, so open the origin again yourself afterwards.
3. The setup page now says *Sign in to finish the setup*. Sign in with your passkey. That account becomes the admin, and the setup code and its files are deleted.

Pocket ID's page for the first account is open to anyone who reaches it until an admin exists, so the ingress answers 403 for it unless the request comes from the setup page. Make the admin account soon after `up`.

From then on only the admin gets a session at the platform. Nobody else gets in, even with a valid account at Pocket ID.

A session ends after `MP_SESSION_IDLE` without a request (default 1 hour), or `MP_SESSION_MAX` after sign in (default 12 hours), whichever comes first. Both are in `config/api.env`. *Sign out* ends your session at once.

### The auth host

Pocket ID answers on its own host name, `auth.<domain>`, never on a path of the main host. Give it its own DNS name: a record for `auth.<domain>` that points to this host, next to the record for `<domain>`. With `platform.localhost`, browsers already resolve `auth.platform.localhost` to this machine. With `MP_INGRESS__TLS_MODE=acme`, the certificate authority must reach both names, so make the record before you run `docker compose up`.

### Who may make an account

Only the admin can make accounts: other people need an invitation. To change this, set `MP_POCKET_ID__SIGNUP` in `.env` to `withToken` (people with a sign up link) or `open` (anyone), run `sh setup.sh` again, which checks the value, then `docker compose up -d`. The default is `disabled`. Any other value stops `setup.sh`. This only decides who may make an account at Pocket ID; the platform still lets in only the admin.

### Lost the setup code, before an admin exists

Nothing is deployed yet, so start the setup again. Delete the files in `secrets/api/setup/` (the files, never the folder), then run `sh setup.sh`. It makes a new code and prints it. `api` reads the files at every call, so it needs no restart.

### Lost the passkey

Pocket ID prints a one hour sign in link for an account:

```sh
docker compose exec pocket-id /app/pocket-id one-time-access-token <user name or email>
```

Open the link it prints (`https://auth.<domain>/lc/<token>`), then register a new passkey. The same command helps if you stopped after making the account and before registering a passkey.

### The static API key

`secrets/pocket-id/static-api-key` is Pocket ID's admin API key. The platform needs it on every `docker compose up`: a short job per client of the platform (the service `pocket-id_provision_api_oidc`, and `pocket-id_revoke_oidc`) uses it to register the platform's own client at Pocket ID. So the file stays after the setup, and every client job mounts it. Anyone who reads it has full admin rights at Pocket ID, so treat it like the CA password: keep `secrets/` private and out of backups you share.

### The client jobs

The jobs above run before `api` starts. When one fails, `api` and the setup page do not start, and `docker compose logs pocket-id_provision_api_oidc` is the only place that says why (the log names the client, never a secret).

A client the platform made carries a marker, and `up` deletes it when its consumer leaves the graph. Make clients you create by hand without that marker, and the platform leaves them alone. A job also removes a secret you added by hand to a client that carries the marker. A client that already exists under the same name without the marker makes the job fail and name it: delete that client in Pocket ID, or name another client id in the graph.

## Change the admin

The admin is bound once. To bind a new admin:

1. Run `docker compose exec api /api admin reset`. It deletes the admin binding and every session. Open screens sign out within 30 seconds.
2. Remove `secrets/api/setup/.setup-done`, run `sh setup.sh` again, which prints a new setup code, then run `docker compose restart api`.
3. Open the origin and go through *Sign in* again. Pocket ID keeps its accounts, so you can use an account that exists.

## Deploy a stack

1. Open a graph in the editor and choose *Deploy*.
2. Read the review. It lists every module with its image, the bindings between modules, and each credential you must supply. Nothing runs yet.
3. Choose *Deploy* in the review. The stack screen shows each step, then each module's state.

If a changed stack fails to start, the platform returns to the last version that worked by itself, and the history says what failed. *Remove* stops the stack and keeps its data, so deploying it again brings it back with its data. *Delete data too* also deletes its volumes and secrets for good.

Deployed stacks restart with Docker. After a host or Docker restart they come back without a new deploy.

### Image versions

Each deploy fixes every image of the stack to the exact version it runs (its digest), and writes that list to `stacks/mp-<stack>/images.lock`. Deploying the same graph again runs the same versions, even when an image's tag now names a newer one. The review shows each image with its version. Choose *Check for newer images* to see where a newer version exists, and tick the ones you want to take.

The deployer keeps the images of the current and the previous deploy on the host, under tags named `localhost/mp-keep/mp-<stack>:<id>`. So a stack that fails after an update returns to the versions that ran before, even when the registry cannot be reached. `docker image prune` leaves these images alone, but `docker image prune -a` removes the ones no container uses at that moment. After that, returning to them needs the registry again.

## What the deployer can do on your host

The `deployer-docker` service holds the Docker socket and runs as root. That gives it full control of the host, so it does as little as possible:

- It runs only the projects `api` sends, and only with the Compose settings the platform renders itself. It refuses anything else, such as privileged mode, host mounts, host networking, devices, added capabilities other than binding low ports, or the Docker socket.
- It writes only inside `stacks/` in this folder, one folder per stack.
- It never joins a deployed stack to the platform's own networks.
- No route from the internet reaches it. Only `api` can call it, with a token and the signed in admin's identity.

Anyone who controls the admin account can run containers on this host. Keep the admin's passkey safe, and register more than one.

## Back up

Back up these, with the platform stopped (`docker compose stop`):

- `.env`, `config/` and `secrets/`: the settings and every platform secret.
- `data/`: the database (with the admin, the sessions and every stack's history), the certificates of the ingress and the certificate issuer's records.
- `stacks/`: each deployed stack's files and secrets.
- The volumes of your deployed stacks, named `mp-<stack>_<volume>`.

## Upgrade

This version has no upgrade command. To upgrade by hand:

1. Back up as above.
2. Unzip the new version over this folder. It replaces the platform's own files. Your `.env`, `secrets/`, `data/` and `stacks/` are not in the zip, so they stay as they are. Compare your `config/` files with the new ones before you let the zip replace them.
3. Run `sh setup.sh` again. It creates only what the new version adds.
4. Run `docker compose up -d --wait --remove-orphans`.

## After *Delete data too*

A stack removed with *Delete data too* keeps its folder in `stacks/` with the files it last ran, so you can see what ran. Its secrets are gone. Running `docker compose` by hand in that folder fails for that reason. Deploy it from the editor instead, which creates new secrets and asks again for the credentials you supply.
