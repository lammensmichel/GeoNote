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
   DATABASE_URL='postgres://…' make migrate-status   # ce qui reste à faire, sans rien changer
   DATABASE_URL='postgres://…' make migrate          # applique ce qui manque
   ```

   Mettez l'URL devant la commande (ne l'exportez pas dans le terminal : un
   `make migrate` « local » viserait sinon la production). Ne la committez jamais.
3. Le script affiche la base visée (sans mot de passe) et demande de taper `oui`
   pour une base distante.
4. Il finit par « PostGIS est actif ». Sinon, il s'arrête avec une erreur.

À relancer après chaque fusion qui ajoute un fichier dans `backend/migrations/`.

## En local (Docker)

Docker crée la base au premier démarrage en lisant tous les fichiers SQL
présents à ce moment-là, mais sans remplir `schema_migrations`. La première fois
seulement, indiquez la dernière migration que votre base contient déjà :

```bash
make migrate-baseline UPTO=007   # marque 001 à 007 comme faites, pas les suivantes
```

Si la base a été créée avant une migration (par exemple sans la table
`reactions` de 006), mettez le numéro d'avant (`UPTO=005`) : le script
appliquera ensuite les suivantes. En cas de doute, une base locale peut être
recréée avec `make dev-reset` (efface les données).

Ensuite, `make migrate` applique les nouvelles migrations comme en production.

## Règles

- Une migration ne se modifie jamais une fois fusionnée : on en ajoute une nouvelle,
  numérotée à la suite (`008_mon_sujet.up.sql`).
- Chaque fichier est appliqué dans une transaction : en cas d'erreur, rien n'est gardé
  et le script s'arrête. Donc pas de `BEGIN;`/`COMMIT;` ni de `CONCURRENTLY`
  dans un fichier (le script les refuse).
- Les données de test (`002_seed_data`) ne sont **jamais** appliquées sans `--seed`,
  et `--seed` n'est accepté que sur `localhost` (comptes au mot de passe connu).
- Lancer le script une seule fois à la fois.
