#!/bin/sh
# Creates the roles and databases of api and authelia when they are missing, with the passwords
# setup.sh wrote. Runs on every `docker compose up`; a second run changes nothing. Passwords are
# read inside psql (\set with backticks), so they never appear on a command line.
set -eu
PGPASSWORD="$(cat /run/secrets/postgres_password)"
export PGPASSWORD
psql -v ON_ERROR_STOP=1 -h postgres -U postgres -d postgres <<'SQL'
\set api_password `cat /run/secrets/api_db_password`
\set authelia_password `cat /run/secrets/authelia_db_password`
SELECT format('CREATE ROLE api LOGIN PASSWORD %L', :'api_password')
  WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'api')\gexec
SELECT format('CREATE ROLE authelia LOGIN PASSWORD %L', :'authelia_password')
  WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'authelia')\gexec
SELECT 'CREATE DATABASE platform_api OWNER api'
  WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'platform_api')\gexec
SELECT 'CREATE DATABASE authelia OWNER authelia'
  WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'authelia')\gexec
SQL
