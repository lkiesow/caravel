-- SQLite has supported DROP COLUMN since 3.35 (2021), and the driver this
-- project uses is far newer.
ALTER TABLE media_assets DROP COLUMN license;
ALTER TABLE media_assets DROP COLUMN credit;
ALTER TABLE media_assets DROP COLUMN source_url;
