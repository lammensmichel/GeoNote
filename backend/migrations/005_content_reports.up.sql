-- =============================================================
-- Migration 005: Content reports (signalements)
-- Cette migration manquait dans le depot : 007_reports la modifie et
-- le code Go l'utilise (interaction_repo.go), mais elle n'avait jamais
-- ete committee. Sans elle, une base neuve ne peut pas etre creee.
-- IF NOT EXISTS : sans effet sur une base ou la table existe deja.
-- =============================================================

CREATE TABLE IF NOT EXISTS content_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    reason VARCHAR(30) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Un utilisateur ne peut signaler un message qu'une fois :
    -- le masquage automatique compte les signalements par message.
    CONSTRAINT content_reports_unique_reporter UNIQUE (message_id, reporter_id)
);

CREATE INDEX IF NOT EXISTS idx_content_reports_reporter ON content_reports(reporter_id);
