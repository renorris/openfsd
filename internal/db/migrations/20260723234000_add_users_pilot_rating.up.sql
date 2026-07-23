-- Pilot rating on user certificates (0 = none / unrated; typical range 0–5).
ALTER TABLE users ADD COLUMN pilot_rating integer NOT NULL DEFAULT 0;
