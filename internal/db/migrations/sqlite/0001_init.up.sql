-- Caravel's schema, SQLite.
--
-- Squashed in Stage 18 from the twelve migrations that built it between Stage
-- 01 and Stage 17, while nothing was deployed anywhere and no database existed
-- that would have to be upgraded. Column order is preserved exactly as the old
-- chain left it -- appended columns stay appended -- so that this is a squash
-- and not also a rewrite: `sqlc generate` produces a byte-identical result
-- against the old chain and this file, which is the strongest available
-- evidence that they describe the same schema.
--
-- What the history contained, for anyone reading a comment here that sounds
-- like it is answering a question nobody asked: the items.category value
-- "location" became "site"; documents became files; trips.notes was replaced by
-- trips.subtitle; and visibility, ownership, admin, settings and the expense
-- tables arrived in Stages 14 and 17. The reasoning behind each is kept below,
-- because that is the part a schema dump destroys.

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    email TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    -- Governs users, not data. An admin gets no access to anyone else's trips
    -- -- Server.tripRole never consults this -- because a "personal" file the
    -- instance operator can read is not a personal file.
    is_admin INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE auth_identities (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    password_hash TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (provider, provider_user_id)
);

CREATE INDEX idx_auth_identities_user_id ON auth_identities(user_id);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    user_agent TEXT,
    ip TEXT
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    start_date TEXT,
    end_date TEXT,
    -- Not a DB-enforced FK: media_assets references trips (every asset
    -- belongs to exactly one trip, for ownership checks at serve time), so a
    -- trips -> media_assets FK here would make the two tables mutually
    -- dependent. Enforced at the application layer instead.
    preview_image_id TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    subtitle TEXT,
    -- One currency per trip, not per expense. A trip is normally spent in one
    -- currency, and making it per-expense makes every total and every balance
    -- per-currency too -- so the common case would pay for the rare one. A
    -- purchase in another currency is entered as the converted amount.
    --
    -- No CHECK: the code validates against an allowlist (see db.Currencies),
    -- because the list of currencies people want will grow and a CHECK would
    -- need a migration each time.
    currency TEXT NOT NULL DEFAULT 'EUR'
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
    created_at TEXT NOT NULL
);

CREATE INDEX idx_media_assets_trip_id ON media_assets(trip_id);

CREATE TABLE items (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    -- "site" rather than "location": the UI calls a whole item a location, so
    -- the category needed a different word (Stage 02).
    category TEXT NOT NULL CHECK (category IN ('site', 'stay', 'transport')),
    type TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    notes TEXT,
    image_id TEXT REFERENCES media_assets(id) ON DELETE SET NULL,
    show_on_map INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_items_trip_id_category ON items(trip_id, category);

CREATE TABLE item_locations (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL UNIQUE REFERENCES items(id) ON DELETE CASCADE,
    lat REAL,
    lng REAL,
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
    start_date TEXT,
    end_date TEXT,
    label TEXT,
    all_day INTEGER NOT NULL DEFAULT 1,
    start_time TEXT,
    end_time TEXT
);

CREATE INDEX idx_item_dates_item_id ON item_dates(item_id);

CREATE TABLE files (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    item_id TEXT REFERENCES items(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    content_type TEXT,
    size_bytes INTEGER NOT NULL,
    uploaded_at TEXT NOT NULL,
    note TEXT,
    -- Two values, where checklists have three. A checklist can be *ticked*, so
    -- "everyone sees it" and "everyone may change it" are different questions;
    -- a file has no equivalent second axis, since who may edit its note or
    -- delete it already follows the trip role.
    --
    -- Default is trip, deliberately: an invisible privacy default produces
    -- "where did my upload go?" rather than safety, and every file on a solo
    -- trip would be born private for no reason. The choice is visible at upload
    -- time instead, and personal rows are marked with a lock.
    visibility TEXT NOT NULL DEFAULT 'trip'
        CHECK (visibility IN ('personal', 'trip')),
    -- Who uploaded it, which is who a personal file belongs to.
    --
    -- Nullable, and it has to stay that way: ON DELETE SET NULL is the point --
    -- deleting an account must not delete a trip's files. (Migration 0009
    -- suggested making this NOT NULL at squash time. It cannot be: NOT NULL and
    -- ON DELETE SET NULL contradict each other, and the delete would fail
    -- instead of blanking the column.) NULL fails closed everywhere it is
    -- compared: the visibility predicate matches owner_user_id against the
    -- reading user, and NULL matches nobody, so a personal file with no owner
    -- is visible to no one rather than to everyone.
    owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_files_trip_id ON files(trip_id);
CREATE INDEX idx_files_item_id ON files(item_id);
CREATE INDEX idx_files_owner_user_id ON files(owner_user_id);

CREATE TABLE itinerary_days (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    date TEXT NOT NULL,
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

CREATE TABLE checklists (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    -- Three states, where files have two:
    --
    --   personal  only its author sees it at all
    --   trip      everyone on the trip sees it, only its author changes it
    --   shared    everyone on the trip sees it and ticks it
    --
    -- The middle one is why this is not the files model with a third name
    -- bolted on: seeing a list and being able to tick it are genuinely
    -- different permissions once more than one person is looking at it.
    --
    -- Default is shared, and it differs from the files default for a reason. A
    -- file is one person's document, so trip-visible-but-not-editable is its
    -- natural resting state. A checklist is a job to be done together, and the
    -- packing list everyone ticks is the case that made anyone want checklists.
    visibility TEXT NOT NULL DEFAULT 'shared'
        CHECK (visibility IN ('personal', 'trip', 'shared')),
    -- Whose list it is. Nullable for the same reason files.owner_user_id is,
    -- and fails closed the same way.
    owner_user_id TEXT REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX idx_checklists_trip_id ON checklists(trip_id);
CREATE INDEX idx_checklists_owner_user_id ON checklists(owner_user_id);

CREATE TABLE checklist_items (
    id TEXT PRIMARY KEY,
    checklist_id TEXT NOT NULL REFERENCES checklists(id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    checked INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_checklist_items_checklist_id ON checklist_items(checklist_id);

-- Trip membership beyond the owner. The owner is NOT stored here: trips.owner_id
-- stays authoritative, so the two can never disagree and no code path can demote
-- an owner by touching a membership. The CHECK makes 'owner' unrepresentable.
CREATE TABLE trip_members (
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('editor', 'viewer')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (trip_id, user_id)
);

-- The trips list asks "which trips is this user a member of?", so user_id needs
-- its own index; trip_id is already covered by the primary key's leading column.
CREATE INDEX idx_trip_members_user_id ON trip_members(user_id);

-- Instance-wide settings an admin can change at runtime, which an environment
-- variable cannot be. A key/value table rather than a column per setting, so
-- the next setting does not need a migration.
--
-- The column is `name`, not `key`: sqlc's sqlite parser mis-lexes `key` in an
-- INSERT ... VALUES clause (and it is reserved in other dialects besides), so
-- the generator cannot produce a setter for it at all.
CREATE TABLE app_settings (
    name TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Registration is closed by default. A fresh instance is still usable:
-- registrationAllowed also permits a signup when no users exist at all, and
-- that first account becomes the admin.
INSERT INTO app_settings (name, value) VALUES ('open_signup', 'false');

-- What a trip cost. Stage 17.
CREATE TABLE expenses (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    title TEXT NOT NULL,

    -- An integer in the minor unit of the trip currency -- cents for EUR, yen
    -- for JPY, which has no minor unit at all. Never a REAL: a ledger that is
    -- out by a cent is worse than one that refuses to exist, and floating
    -- point money is how you get one. The exponent is a display concern and
    -- lives in the client, which reads it from Intl rather than assuming two.
    --
    -- Positive only. Refunds and negative amounts are deliberately out of
    -- scope for now, and a zero-cost row is a note rather than an expense.
    amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),

    -- A calendar day, as YYYY-MM-DD, matching trips.start_date and
    -- itinerary_days.date. Same reasoning as those: it is the day you spent
    -- the money, not an instant, so storing it as a timestamp would invent a
    -- time zone nobody chose.
    spent_on TEXT NOT NULL,

    -- Who paid. Nullable with ON DELETE SET NULL, exactly as files.owner_user_id
    -- and checklists.owner_user_id are: deleting an account must not delete the
    -- trip ledger.
    --
    -- Unlike those two, NULL here does not fail closed. The expense stays
    -- visible to everyone on the trip and still counts toward the total; it is
    -- shown as unattributed, and the balances view reports it rather than
    -- splitting it, since a payer nobody can name cannot be credited.
    payer_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,

    created_at TEXT NOT NULL
);

CREATE INDEX idx_expenses_trip_id ON expenses(trip_id);
CREATE INDEX idx_expenses_payer_user_id ON expenses(payer_user_id);

-- Who an expense was for, as opposed to who paid for it.
--
-- No amount column, deliberately. The split is equal among the rows present and
-- is computed when read. Storing a share list AND a per-share amount is two
-- sources of truth for one number, and they drift the first time somebody is
-- added to an expense -- at which point the stored amounts are silently wrong
-- and nothing says so.
--
-- No rows at all for an expense means everyone on the trip, resolved at read
-- time rather than written out at create time. That keeps the common case free
-- of writes, and means adding a member does not require rewriting history. The
-- consequence is worth stating plainly: a new member's share of past expenses
-- appears retroactively. An explicit set of rows is how you pin an expense to a
-- subset of the trip.
--
-- The primary key is the pair, so a client that names the same person twice
-- cannot double their weight in the split.
CREATE TABLE expense_shares (
    expense_id TEXT NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (expense_id, user_id)
);

-- The expense_id half of the key already serves lookups by expense. This is
-- for the other direction, which is what happens when somebody leaves a trip.
CREATE INDEX idx_expense_shares_user_id ON expense_shares(user_id);
