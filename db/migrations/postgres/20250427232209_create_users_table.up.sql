create sequence public.users_cid_seq
    as integer;

create table public.users
(
    cid            serial
        constraint users_pk
        default nextval('users_cid_seq')
        primary key,
    password       char(60) not null,
    first_name     varchar(255),
    last_name      varchar(255),
    network_rating smallint not null
);
