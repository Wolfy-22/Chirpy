-- +goose Up
create table users (
    id UUID primary key,
    created_at TIMESTAMP not null,
    updated_at TIMESTAMP not null,
    email TEXT not null unique,
    
    -- new--
    hashed_password TEXT not null default 'unset'
);

-- +goose Down
drop table users;