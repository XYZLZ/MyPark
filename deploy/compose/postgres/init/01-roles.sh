#!/bin/sh
# Creates the MyPark database roles. The postgres image runs this once, as
# the superuser, when it initializes an empty data directory. CI runs it
# against its service container too, so it is safe to run more than once.
#
#   mypark_migrator  LOGIN, owner of $POSTGRES_DB: runs the migrations (DDL).
#   mypark_app       LOGIN, DML only: used by the api, worker and seed.
#
# Passwords come from MYPARK_MIGRATOR_PASSWORD and MYPARK_APP_PASSWORD. psql
# reads them with \getenv, so they never appear on a command line or in the
# shell's expansion of the SQL below.
set -eu

: "${MYPARK_MIGRATOR_PASSWORD:?MYPARK_MIGRATOR_PASSWORD is required}"
: "${MYPARK_APP_PASSWORD:?MYPARK_APP_PASSWORD is required}"
: "${POSTGRES_DB:?POSTGRES_DB is required}"

psql -v ON_ERROR_STOP=1 --no-psqlrc \
	--username "${POSTGRES_USER:-postgres}" --dbname "$POSTGRES_DB" <<'EOSQL'
\getenv migrator_password MYPARK_MIGRATOR_PASSWORD
\getenv app_password MYPARK_APP_PASSWORD
\getenv db_name POSTGRES_DB

select 'create role mypark_migrator'
where not exists (select from pg_roles where rolname = 'mypark_migrator')
\gexec
select 'create role mypark_app'
where not exists (select from pg_roles where rolname = 'mypark_app')
\gexec

alter role mypark_migrator with login nosuperuser nocreatedb nocreaterole noreplication password :'migrator_password';
alter role mypark_app with login nosuperuser nocreatedb nocreaterole noreplication password :'app_password';

alter database :"db_name" owner to mypark_migrator;
EOSQL
