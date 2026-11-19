-- =============================================================
-- Migration 008: suppression de compte (RGPD, ticket #4)
-- DELETE /api/v1/me supprime la ligne users ; toutes les donnees
-- liees doivent partir avec elle (ON DELETE CASCADE).
-- reactions.user_id (migration 006) n'avait pas de CASCADE :
-- la suppression d'un utilisateur ayant reagi echouait.
-- Les autres tables liees a users ont deja ON DELETE CASCADE.
-- =============================================================

ALTER TABLE reactions DROP CONSTRAINT IF EXISTS reactions_user_id_fkey;
ALTER TABLE reactions
    ADD CONSTRAINT reactions_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- content_reports a ete cree hors migrations sur certaines bases
-- (voir 005) : on force le CASCADE sur reporter_id, sinon un utilisateur
-- ayant signale un message ne pourrait pas etre supprime.
ALTER TABLE content_reports DROP CONSTRAINT IF EXISTS content_reports_reporter_id_fkey;
ALTER TABLE content_reports
    ADD CONSTRAINT content_reports_reporter_id_fkey
    FOREIGN KEY (reporter_id) REFERENCES users(id) ON DELETE CASCADE;
