# Modular Platform, managed mode

This folder runs the Modular Platform on your own host. Once it runs, you sign in as the admin, plan a stack in the editor, deploy it to this host, and watch each module's state live.

You need a Linux host with Docker Engine and the Docker Compose plugin. Nothing else.

## Install

1. Unzip `modular-platform-<version>.zip` where the platform should live, for example `/opt/modular-platform`, and open that folder.
2. Run `sh setup.sh`. It asks four things:
   - the domain the platform answers on (default `platform.localhost`; it must contain a dot),
   - the admin email,
   - the admin user name,
   - the admin password (at least 12 characters; it is not shown as you type).

   It writes `.env` and creates every secret in `secrets/`. It never prints a secret. Running it again changes no file that exists.
3. Run `docker compose up -d`.
4. Open `https://<your domain>/`.

The platform makes its own certificate authority for HTTPS (`PLATFORM_TLS=internal`), so your browser warns about the certificate the first time. For a public certificate, set `PLATFORM_TLS=acme` in `.env` (the domain must be public), then run `docker compose up -d` again.

Every setting is in `.env`. [`.env.example`](.env.example) lists each one with its default and what it does. Never put a secret in `.env`.

## Sign in

Choose *Sign in* and sign in with the admin user name and password you gave `setup.sh`.

The first sign in whose email equals `PLATFORM_ADMIN_EMAIL` and is verified becomes the admin. From then on only that account gets in. Nobody else gets a session, even with a valid account at the identity provider.

A session ends after `PLATFORM_SESSION_IDLE` without a request (default 1 hour), or `PLATFORM_SESSION_MAX` after sign in (default 12 hours), whichever comes first.

*Sign out* ends your session at once, and the next sign in asks for your password again. The bundled identity provider (Authelia) has no sign out of its own, so its own session cookie stays in the browser until it expires. On a shared computer, close the browser after you sign out.

## Change the admin

The admin is bound once. Changing `PLATFORM_ADMIN_EMAIL` alone does nothing. To bind a new admin:

1. Change `PLATFORM_ADMIN_EMAIL` in `.env` if the admin email changes, then run `docker compose up -d`.
2. Run `docker compose exec api /api admin reset`. It deletes the admin binding and every session and prints one line. Open screens sign out within 30 seconds.
3. Sign in with the account whose verified email is `PLATFORM_ADMIN_EMAIL`. It becomes the admin.

## Deploy a stack

1. Open a graph in the editor and choose *Deploy*.
2. Read the review. It lists every module with its image, the bindings between modules, and each credential you must supply. Nothing runs yet.
3. Choose *Deploy* in the review. The deployment screen shows each step, then each module's state.

If a changed stack fails to start, the platform returns to the last version that worked by itself, and the history says what failed. *Remove* stops the stack and keeps its data, so deploying it again brings it back with its data. *Delete data too* also deletes its volumes and secrets for good.

Deployed stacks restart with Docker. After a host or Docker restart they come back without a new deploy.

## What the deployer can do on your host

The `deployer-docker` service holds the Docker socket and runs as root. That gives it full control of the host, so it does as little as possible:

- It runs only the projects `api` sends, and only with the Compose settings the platform renders itself. It refuses anything else, such as privileged mode, host mounts, host networking, devices, added capabilities, or the Docker socket.
- It writes only inside `deployments/` in this folder, one folder per deployment.
- It never joins a deployed stack to the platform's own networks.
- No route from the internet reaches it. Only `api` can call it, with a token and the signed in admin's identity.

Anyone who controls the admin account can run containers on this host. Keep the admin password strong.

## Back up

Back up these, with the platform stopped (`docker compose stop`):

- `.env` and `secrets/`: the settings and every platform secret.
- `deployments/`: each deployed stack's files and secrets.
- `data/`: the certificate issuer's records.
- The Docker volumes `modular-platform_pg_data` (the database with the admin, the sessions and every deployment's history), `modular-platform_caddy_data` and `modular-platform_caddy_config` (the certificates of the ingress).
- The volumes of your deployed stacks, named `mp-<deployment>_<volume>`.

## Upgrade

This version has no upgrade command. To upgrade by hand:

1. Back up as above.
2. Unzip the new version over this folder. It replaces the platform's own files. Your `.env`, `secrets/`, `data/` and `deployments/` are not in the zip, so they stay as they are.
3. Run `sh setup.sh` again. It creates only what the new version adds.
4. Run `docker compose up -d`.

## After *Delete data too*

A deployment removed with *Delete data too* keeps its folder in `deployments/` with the files it last ran, so you can see what ran. Its secrets are gone. Running `docker compose` by hand in that folder fails for that reason. Deploy it from the editor instead, which creates new secrets and asks again for the credentials you supply.
