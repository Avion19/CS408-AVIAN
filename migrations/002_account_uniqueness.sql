-- Enforce case-insensitive identity uniqueness even outside form handlers.
CREATE UNIQUE INDEX users_email_casefold ON users(lower(email));
CREATE UNIQUE INDEX members_username_casefold ON members(lower(username));
