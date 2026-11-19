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
