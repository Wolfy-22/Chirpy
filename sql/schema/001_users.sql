-- +goose Up
create table users (
    id UUID primary key,
    created_at TIMESTAMP not null,
    updated_at TIMESTAMP not null,
    email TEXT not null unique,
    
    -- new--
    hashed_password TEXT not null default 'unset',

    -- new --
    is_chirpy_red boolean not null default false
);

-- +goose Down
drop table users;