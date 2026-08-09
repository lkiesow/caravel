CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    email TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE auth_identities (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    password_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (provider, provider_user_id)
);

CREATE INDEX idx_auth_identities_user_id ON auth_identities(user_id);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    user_agent TEXT,
    ip TEXT
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    start_date DATE,
    end_date DATE,
    -- Not a DB-enforced FK: media_assets references trips (every asset
    -- belongs to exactly one trip, for ownership checks at serve time), so a
    -- trips -> media_assets FK here would make the two tables mutually
    -- dependent. Enforced at the application layer instead.
    preview_image_id TEXT,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_trips_owner_id ON trips(owner_id);

CREATE TABLE media_assets (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('upload', 'url')),
    storage_path TEXT,
    external_url TEXT,
    content_type TEXT,
    width INTEGER,
    height INTEGER,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_media_assets_trip_id ON media_assets(trip_id);

CREATE TABLE items (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    category TEXT NOT NULL CHECK (category IN ('location', 'stay', 'transport')),
    type TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    notes TEXT,
    image_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL,
    show_on_map BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_items_trip_id_category ON items(trip_id, category);

CREATE TABLE item_locations (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL UNIQUE REFERENCES items(id) ON DELETE CASCADE,
    lat DOUBLE PRECISION,
    lng DOUBLE PRECISION,
    address TEXT
);

CREATE TABLE item_links (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    label TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_item_links_item_id ON item_links(item_id);

CREATE TABLE item_dates (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    start_date DATE,
    end_date DATE,
    label TEXT,
    all_day BOOLEAN NOT NULL DEFAULT TRUE,
    start_time TEXT,
    end_time TEXT
);

CREATE INDEX idx_item_dates_item_id ON item_dates(item_id);

CREATE TABLE documents (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    item_id TEXT REFERENCES items(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    content_type TEXT,
    size_bytes BIGINT NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_documents_trip_id ON documents(trip_id);
CREATE INDEX idx_documents_item_id ON documents(item_id);

CREATE TABLE itinerary_days (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    notes TEXT,
    UNIQUE (trip_id, date)
);

CREATE TABLE itinerary_entries (
    id TEXT PRIMARY KEY,
    itinerary_day_id TEXT NOT NULL REFERENCES itinerary_days(id) ON DELETE CASCADE,
    item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    note TEXT
);

CREATE INDEX idx_itinerary_entries_day_id ON itinerary_entries(itinerary_day_id);
