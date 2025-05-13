create table users
(
    cid            integer  not null
        constraint users_pk
        primary key autoincrement,
    password       text(60) not null,
    first_name     text(255),
    last_name      text(255),
    network_rating integer  not null
);
