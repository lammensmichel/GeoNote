#!/usr/bin/env bash
# Applique les migrations SQL de backend/migrations sur une base PostgreSQL
# (Neon en production, Docker en local). Chaque migration est appliquee une
# seule fois : la table schema_migrations garde la liste de ce qui est fait.
#
# Usage :
#   DATABASE_URL=postgres://... scripts/migrate.sh             # applique ce qui manque
#   DATABASE_URL=postgres://... scripts/migrate.sh --status    # affiche l'etat, ne change rien
#   DATABASE_URL=postgres://... scripts/migrate.sh --baseline  # base deja creee a la main :
#                                                              # marque tout comme applique
#   DATABASE_URL=postgres://... scripts/migrate.sh --seed      # ajoute les donnees de test
#                                                              # (refuse hors base locale)
#
# URL utilisee, dans cet ordre : MIGRATE_DATABASE_URL, DATABASE_URL_UNPOOLED
# (fournie par l'integration Neon de Vercel, connexion directe sans pooler),
# puis DATABASE_URL.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-$SCRIPT_DIR/../backend/migrations}"

MODE="apply"
WITH_SEED=0

for arg in "$@"; do
  case "$arg" in
    --status) MODE="status" ;;
    --baseline) MODE="baseline" ;;
    --seed) WITH_SEED=1 ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Option inconnue : $arg (voir --help)" >&2; exit 2 ;;
  esac
done

DB_URL="${MIGRATE_DATABASE_URL:-${DATABASE_URL_UNPOOLED:-${DATABASE_URL:-}}}"
if [[ -z "$DB_URL" ]]; then
  echo "Erreur : DATABASE_URL n'est pas defini." >&2
  exit 1
fi

if ! command -v psql >/dev/null 2>&1; then
  echo "Erreur : psql est introuvable. Installez le client PostgreSQL." >&2
  exit 1
fi

if [[ ! -d "$MIGRATIONS_DIR" ]]; then
  echo "Erreur : dossier de migrations introuvable : $MIGRATIONS_DIR" >&2
  exit 1
fi

# Les donnees de test creent des comptes avec un mot de passe connu :
# jamais sur une base distante.
is_local_db() {
  [[ "$1" =~ @(localhost|127\.0\.0\.1|\[::1\]|db)(:[0-9]+)?(/|\?|$) ]]
}

if [[ "$WITH_SEED" == 1 ]] && ! is_local_db "$DB_URL"; then
  echo "Erreur : --seed est refuse sur une base qui n'est pas locale (comptes de test au mot de passe connu)." >&2
  exit 1
fi

# Affiche la base visee, sans le mot de passe.
echo "Base : $(sed -E 's#://[^@/]*@#://***@#; s#\?.*$##' <<<"$DB_URL")"

run_psql() {
  psql "$DB_URL" -X -q -v ON_ERROR_STOP=1 "$@"
}

query() {
  run_psql -A -t -c "$1"
}

is_seed() {
  [[ "$1" == *seed* ]]
}

# Liste des migrations, triee par nom (001_, 002_, ...).
mapfile -t FILES < <(find "$MIGRATIONS_DIR" -maxdepth 1 -type f -name '*.up.sql' | LC_ALL=C sort)
if [[ ${#FILES[@]} -eq 0 ]]; then
  echo "Aucune migration trouvee dans $MIGRATIONS_DIR." >&2
  exit 1
fi

for f in "${FILES[@]}"; do
  v="$(basename "$f" .up.sql)"
  if [[ ! "$v" =~ ^[0-9]{3}_[a-z0-9_]+$ ]]; then
    echo "Erreur : nom de migration invalide : $(basename "$f") (attendu : 008_mon_sujet.up.sql)" >&2
    exit 1
  fi
done

has_table="$(query "SELECT to_regclass('public.schema_migrations') IS NOT NULL;")"
if [[ "$has_table" != "t" ]] && [[ "$MODE" != "status" ]]; then
  query "CREATE TABLE schema_migrations (
           version TEXT PRIMARY KEY,
           applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
         );" >/dev/null
  has_table="t"
fi
if [[ "$has_table" == "t" ]]; then
  APPLIED="$(query "SELECT version FROM schema_migrations;")"
else
  APPLIED=""
fi

is_applied() {
  grep -qxF "$1" <<<"$APPLIED"
}

case "$MODE" in
  status)
    pending=0
    for f in "${FILES[@]}"; do
      v="$(basename "$f" .up.sql)"
      if is_applied "$v"; then
        echo "  applique   $v"
      elif is_seed "$v"; then
        echo "  ignore     $v (donnees de test, avec --seed en local)"
      else
        echo "  a faire    $v"
        pending=$((pending + 1))
      fi
    done
    echo "$pending migration(s) a appliquer."
    ;;

  baseline)
    if [[ "$(query "SELECT to_regclass('public.users') IS NOT NULL;")" != "t" ]]; then
      echo "Erreur : la base est vide. --baseline sert seulement pour une base deja creee ; lancez le script sans option." >&2
      exit 1
    fi
    for f in "${FILES[@]}"; do
      v="$(basename "$f" .up.sql)"
      if is_seed "$v" || is_applied "$v"; then
        continue
      fi
      query "INSERT INTO schema_migrations (version) VALUES ('$v') ON CONFLICT DO NOTHING;" >/dev/null
      echo "  marque     $v"
    done
    echo "Base marquee comme a jour. Les prochaines migrations seront appliquees normalement."
    ;;

  apply)
    if [[ -z "$APPLIED" ]] && [[ "$(query "SELECT to_regclass('public.users') IS NOT NULL;")" == "t" ]]; then
      echo "Erreur : la base contient deja des tables mais aucune migration n'est enregistree." >&2
      echo "Si elle a ete creee par Docker, lancez d'abord : scripts/migrate.sh --baseline" >&2
      exit 1
    fi
    count=0
    for f in "${FILES[@]}"; do
      v="$(basename "$f" .up.sql)"
      if is_applied "$v"; then
        continue
      fi
      if is_seed "$v" && [[ "$WITH_SEED" != 1 ]]; then
        continue
      fi
      echo "  application $v"
      # Le fichier et son enregistrement passent dans une seule transaction :
      # en cas d'erreur, rien n'est garde.
      run_psql --single-transaction \
        -c "SET LOCAL client_min_messages = warning;" \
        -f "$f" \
        -c "INSERT INTO schema_migrations (version) VALUES ('$v');"
      count=$((count + 1))
    done
    if [[ "$(query "SELECT count(*) FROM pg_extension WHERE extname = 'postgis';")" != "1" ]]; then
      echo "Erreur : l'extension PostGIS n'est pas active sur cette base." >&2
      exit 1
    fi
    echo "$count migration(s) appliquee(s). PostGIS est actif."
    ;;
esac
