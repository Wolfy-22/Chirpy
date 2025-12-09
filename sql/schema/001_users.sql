-- +goose Up
create table users (
    id UUID primary key,
    created_at TIMESTAMP not null,
    updated_at TIMESTAMP not null,
    email TEXT not null unique
);

-- +goose Down
drop table users;