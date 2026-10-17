-- Checkpoint 1 schema. Timestamps are UTC ISO 8601 text; application handlers
-- will enforce ownership, membership, and content visibility in later stages.
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE members (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    username TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    region TEXT NOT NULL DEFAULT '',
    bio TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    csrf_token TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE species (
    id INTEGER PRIMARY KEY,
    common_name TEXT NOT NULL UNIQUE,
    scientific_name TEXT NOT NULL,
    description TEXT NOT NULL,
    habitat TEXT NOT NULL,
    image_path TEXT,
    source_name TEXT NOT NULL,
    source_url TEXT NOT NULL
);

CREATE TABLE flocks (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    region TEXT NOT NULL,
    topic TEXT NOT NULL,
    description TEXT NOT NULL,
    image_path TEXT,
    created_by INTEGER NOT NULL REFERENCES members(id),
    created_at TEXT NOT NULL,
    is_closed INTEGER NOT NULL DEFAULT 0 CHECK (is_closed IN (0, 1))
);

CREATE TABLE flock_members (
    flock_id INTEGER NOT NULL REFERENCES flocks(id) ON DELETE CASCADE,
    member_id INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    joined_at TEXT NOT NULL,
    PRIMARY KEY (flock_id, member_id)
);
-- SQLite enforces at most one owner. Flock creation must insert the first owner
-- in the same transaction, and later transfers must retain one owner.
CREATE UNIQUE INDEX one_owner_per_flock ON flock_members(flock_id) WHERE role = 'owner';

CREATE TABLE chirps (
    id INTEGER PRIMARY KEY,
    member_id INTEGER NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    flock_id INTEGER REFERENCES flocks(id) ON DELETE SET NULL,
    content TEXT NOT NULL,
    image_path TEXT,
    visibility TEXT NOT NULL CHECK (visibility IN ('public', 'flock')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (visibility = 'public' OR flock_id IS NOT NULL)
);
-- Before deleting a flock, the app must delete its flock-only chirps. SQLite
-- then unlinks public chirps through ON DELETE SET NULL above.

CREATE TABLE chirp_species (
    chirp_id INTEGER NOT NULL REFERENCES chirps(id) ON DELETE CASCADE,
    species_id INTEGER NOT NULL REFERENCES species(id),
    PRIMARY KEY (chirp_id, species_id)
);

CREATE INDEX sessions_by_user ON sessions(user_id);
CREATE INDEX flock_members_by_member ON flock_members(member_id);
CREATE INDEX chirps_newest ON chirps(created_at DESC, id DESC);
CREATE INDEX chirps_by_member ON chirps(member_id, created_at DESC);
CREATE INDEX chirps_by_flock ON chirps(flock_id, created_at DESC);
CREATE INDEX chirp_species_by_species ON chirp_species(species_id);
