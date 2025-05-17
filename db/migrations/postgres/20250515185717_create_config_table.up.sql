create table config
(
    key   varchar not null,
    value varchar not null
);

create unique index config_key_uindex
    on config (key);
