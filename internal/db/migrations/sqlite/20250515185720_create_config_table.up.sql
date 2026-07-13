create table config
(
    key   text not null,
    value text not null
);

create unique index config_key_uindex
    on config (key);
