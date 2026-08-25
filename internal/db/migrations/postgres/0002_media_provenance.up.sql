-- Where a stored image came from, and who is owed the credit.
--
-- media_assets has recorded what an image *is* since 0001 -- its bytes, its
-- type, its size -- and nothing about where it came from. That was fine while
-- every image was one a person had chosen and uploaded themselves. It stops
-- being fine now that the assistant proposes covers it found on the web: a
-- Wikimedia photograph is freely licensed, not unencumbered, and nearly all of
-- them require attribution.
--
-- Caravel is already multi-user, and the backlog carries public share links.
-- An image stored with no record of its origin is a problem waiting for the
-- day somebody shares a trip, and it is not recoverable after the fact.
--
-- All three are nullable, because every asset that exists today has none, and
-- so does every image somebody uploads from their own camera.
ALTER TABLE media_assets ADD COLUMN source_url TEXT;
ALTER TABLE media_assets ADD COLUMN credit TEXT;
ALTER TABLE media_assets ADD COLUMN license TEXT;
