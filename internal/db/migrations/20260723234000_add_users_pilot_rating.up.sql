-- VATSIM pilot rating wire IDs: 0,1,3,7,15,31,63 (P0/PPL/IR/CMEL/ATPL/FI/FE).
-- See pkg/protocol.PilotRating / docs/enumerations.md.
ALTER TABLE users ADD COLUMN pilot_rating integer NOT NULL DEFAULT 0;
