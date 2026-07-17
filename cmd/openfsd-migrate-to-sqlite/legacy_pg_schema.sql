-- Historical openfsd PostgreSQL schema (pre-SQLite-only), adjusted only where
-- modern Postgres rejects the original dual-default SERIAL definition.
-- Used by the migrator E2E to seed an ephemeral source database.

create table public.users
(
    cid            serial
        constraint users_pk
        primary key,
    password       char(60) not null,
    first_name     varchar(255),
    last_name      varchar(255),
    network_rating smallint not null
);

create table config
(
    key   varchar not null,
    value varchar not null
);

create unique index config_key_uindex
    on config (key);
