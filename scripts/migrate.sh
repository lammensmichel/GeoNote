#!/usr/bin/env bash
# Applique les migrations SQL de backend/migrations sur une base PostgreSQL
# (Neon en production, Docker en local). Chaque migration est appliquee une
# seule fois : la table schema_migrations garde la liste de ce qui est fait.
#
# Usage :
#   DATABASE_URL=postgres://... scripts/migrate.sh                  # applique ce qui manque
#   DATABASE_URL=postgres://... scripts/migrate.sh --status         # etat, ne change rien
#   DATABASE_URL=postgres://... scripts/migrate.sh --baseline=007   # base deja creee a la main :
#                                     # marque 001 a 007 comme appliquees (pas au-dela)
#   DATABASE_URL=postgres://... scripts/migrate.sh --seed           # + donnees de test
#                                     # (refuse hors base locale)
#   Option --yes : confirme une base distante sans question (ex. automatisation).
#
# URL utilisee : MIGRATE_DATABASE_URL, sinon DATABASE_URL. Pour Neon, prendre
# l'URL directe (DATABASE_URL_UNPOOLED dans Vercel), pas celle du pooler.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-$SCRIPT_DIR/../backend/migrations}"

MODE="apply"
WITH_SEED=0
ASSUME_YES=0
BASELINE_UPTO=""

for arg in "$@"; do
  case "$arg" in
    --status) MODE="status" ;;
    --baseline=*) MODE="baseline"; BASELINE_UPTO="${arg#--baseline=}" ;;
    --baseline)
      echo "Erreur : indiquez la derniere migration deja presente, ex. --baseline=007." >&2
      exit 2 ;;
    --seed) WITH_SEED=1 ;;
    --yes) ASSUME_YES=1 ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Option inconnue : $arg (voir --help)" >&2; exit 2 ;;
  esac
done

if [[ "$MODE" == "baseline" ]] && [[ ! "$BASELINE_UPTO" =~ ^[0-9]{3}$ ]]; then
  echo "Erreur : --baseline attend un numero a 3 chiffres, ex. --baseline=007." >&2
  exit 2
fi

DB_URL="${MIGRATE_DATABASE_URL:-${DATABASE_URL:-}}"
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

# Vrai seulement pour une URL postgres:// dont l'hote est cette machine.
# Les chaines "cle=valeur" et les URL avec host=/hostaddr= en parametre
# (qui remplacent l'hote) sont considerees comme distantes.
is_local_db() {
  local url="$1" rest hostport host
  [[ "$url" =~ ^postgres(ql)?:// ]] || return 1
  [[ "$url" =~ [?\&](host|hostaddr|service)= ]] && return 1
  rest="${url#*://}"
  hostport="${rest%%[/?]*}"
  hostport="${hostport##*@}"
  [[ "$hostport" == *,* ]] && return 1
  if [[ "$hostport" == \[* ]]; then
    host="${hostport%%]*}]"
  else
    host="${hostport%%:*}"
  fi
  [[ "$host" == "localhost" || "$host" == "127.0.0.1" || "$host" == "[::1]" ]]
}

redacted_url() {
  if [[ "$1" =~ ^postgres(ql)?:// ]]; then
    sed -E 's#://.*@#://***@#; s#\?.*$##' <<<"$1"
  else
    echo "(chaine de connexion masquee)"
  fi
}

if [[ "$WITH_SEED" == 1 ]] && ! is_local_db "$DB_URL"; then
  echo "Erreur : --seed est refuse sur une base qui n'est pas locale (comptes de test au mot de passe connu)." >&2
  exit 1
fi

echo "Base : $(redacted_url "$DB_URL")"

# Une base distante (Neon...) doit etre confirmee avant toute modification.
if [[ "$MODE" != "status" ]] && ! is_local_db "$DB_URL" && [[ "$ASSUME_YES" != 1 ]]; then
  if [[ -t 0 ]]; then
    read -r -p "Base distante. Tapez 'oui' pour continuer : " answer
    if [[ "$answer" != "oui" ]]; then
      echo "Annule." >&2
      exit 1
    fi
  else
    echo "Erreur : base distante sans terminal. Relancez avec --yes si c'est voulu." >&2
    exit 1
  fi
fi

run_psql() {
  psql "$DB_URL" -X -q -v ON_ERROR_STOP=1 "$@"
}

query() {
  run_psql -A -t -c "$1"
}

# Seules les donnees de test (NNN_seed_data) sont exclues par defaut.
is_seed() {
  [[ "$1" =~ ^[0-9]{3}_seed_data$ ]]
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
  # Le script entoure chaque fichier d'une transaction : un BEGIN/COMMIT
  # explicite ou un CONCURRENTLY la casserait (application partielle).
  if grep -Eiq '^[[:space:]]*(BEGIN|COMMIT|ROLLBACK|START[[:space:]]+TRANSACTION)[[:space:]]*;|CONCURRENTLY' "$f"; then
    echo "Erreur : $(basename "$f") contient BEGIN/COMMIT ou CONCURRENTLY, interdit dans une migration." >&2
    exit 1
  fi
done

has_table="$(query "SELECT to_regclass('public.schema_migrations') IS NOT NULL;")"
has_users="$(query "SELECT to_regclass('public.users') IS NOT NULL;")"
if [[ "$has_table" != "t" ]] && [[ "$MODE" != "status" ]]; then
  query "CREATE TABLE IF NOT EXISTS schema_migrations (
           version TEXT PRIMARY KEY,
           applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
         );" >/dev/null
  has_table="t"
fi

APPLIED=""
if [[ "$has_table" == "t" ]]; then
  APPLIED="$(query "SELECT version FROM schema_migrations;")"
fi

is_applied() {
  grep -qxF "$1" <<<"$APPLIED"
}

case "$MODE" in
  status)
    if [[ -z "$APPLIED" ]] && [[ "$has_users" == "t" ]]; then
      echo "Attention : la base contient des tables mais aucune migration enregistree (voir --baseline)."
    fi
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
    if [[ "$has_users" != "t" ]]; then
      echo "Erreur : la base est vide. --baseline sert seulement pour une base deja creee ; lancez le script sans option." >&2
      exit 1
    fi
    seed_present="$(query "SELECT EXISTS (SELECT 1 FROM users WHERE email = 'alice@test.com');")"
    for f in "${FILES[@]}"; do
      v="$(basename "$f" .up.sql)"
      if [[ "${v:0:3}" > "$BASELINE_UPTO" ]] || is_applied "$v"; then
        continue
      fi
      # Les donnees de test ne sont marquees que si elles sont deja la.
      if is_seed "$v" && [[ "$seed_present" != "t" ]]; then
        continue
      fi
      query "INSERT INTO schema_migrations (version) VALUES ('$v') ON CONFLICT DO NOTHING;" >/dev/null
      echo "  marque     $v"
    done
    echo "Migrations jusqu'a $BASELINE_UPTO marquees comme appliquees. Lancez maintenant le script sans option pour la suite."
    ;;

  apply)
    if [[ -z "$APPLIED" ]] && [[ "$has_users" == "t" ]]; then
      echo "Erreur : la base contient deja des tables mais aucune migration n'est enregistree." >&2
      echo "Verifiez quelle est la derniere migration presente, puis : scripts/migrate.sh --baseline=NNN" >&2
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
    postgis="$(query "SELECT count(*) FROM pg_extension WHERE extname = 'postgis';")"
    if [[ "$postgis" != "1" ]]; then
      echo "Erreur : l'extension PostGIS n'est pas active sur cette base." >&2
      exit 1
    fi
    echo "$count migration(s) appliquee(s). PostGIS est actif."
    ;;
esac
