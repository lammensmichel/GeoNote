# Migrations de la base de données

Les fichiers SQL sont dans `backend/migrations/` (`001_…up.sql`, `002_…`, …).
Le script `scripts/migrate.sh` les applique dans l'ordre, **une seule fois chacun**.
Il note ce qui est fait dans la table `schema_migrations`.

Il faut le client `psql` (paquet `postgresql-client`, ou `brew install libpq` sur Mac).

## Production (Neon)

1. Dans Vercel → projet API → *Settings* → *Environment Variables*, copier la valeur
   de `DATABASE_URL_UNPOOLED` (connexion directe, sans pooler : à préférer pour les migrations).
2. Dans un terminal, à la racine du dépôt :

   ```bash
   export DATABASE_URL_UNPOOLED='postgres://…'   # ne jamais la committer
   make migrate-status   # ce qui reste à faire, sans rien changer
   make migrate          # applique ce qui manque
   ```

3. Le script finit par « PostGIS est actif ». Sinon, il s'arrête avec une erreur.

À relancer après chaque fusion qui ajoute un fichier dans `backend/migrations/`.

## En local (Docker)

Docker crée la base au premier démarrage en lisant tous les fichiers SQL,
mais sans remplir `schema_migrations`. La première fois seulement :

```bash
make migrate-baseline   # marque les migrations existantes comme faites
```

Ensuite, `make migrate` applique les nouvelles migrations comme en production.

## Règles

- Une migration ne se modifie jamais une fois fusionnée : on en ajoute une nouvelle,
  numérotée à la suite (`008_mon_sujet.up.sql`).
- Chaque fichier est appliqué dans une transaction : en cas d'erreur, rien n'est gardé
  et le script s'arrête.
- Les données de test (`002_seed_data`) ne sont **jamais** appliquées sans `--seed`,
  et `--seed` est refusé sur une base distante (comptes au mot de passe connu).
- Lancer le script une seule fois à la fois.
