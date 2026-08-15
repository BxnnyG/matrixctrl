-- Etappe 36: a second authorization flow shares the login's redirect URI.
--
-- MAS validates redirect_uris strictly and the static client is registered with
-- exactly one. Registering a second would mean editing the MAS client config and
-- redeploying ESS to add a page — too much blast radius for a feature.
--
-- So both flows come back to the same callback and are told apart by the state they
-- carry. The purpose is stored server-side rather than encoded in the state value:
-- the state is the CSRF token, and a value the client could read and change is not
-- one to branch authorization decisions on.
--
-- 'login' is the default so every row written by the existing flow keeps working
-- without touching its INSERT.

ALTER TABLE oidc_states
    ADD COLUMN IF NOT EXISTS purpose text NOT NULL DEFAULT 'login';
