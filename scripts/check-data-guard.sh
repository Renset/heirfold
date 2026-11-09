#!/usr/bin/env bash
# Fails if the repository, or any commit in its history, holds a file type
# that can contain a family tree or a database. The tests and the demo create
# their data at run time, so no such file has a reason to exist here.
set -euo pipefail

forbidden='\.(gramps|gpkg|ged|gedcom|paf|ftm|ftw|grdb|sqlite|sqlite3|db|db-wal|db-shm)$'

status=0

tracked=$(git ls-files | grep -Ei "$forbidden" || true)
if [ -n "$tracked" ]; then
  echo "Forbidden files are tracked:" >&2
  echo "$tracked" >&2
  status=1
fi

history=$(git log --all --pretty=format: --name-only --diff-filter=AM | sort -u | grep -Ei "$forbidden" || true)
if [ -n "$history" ]; then
  echo "Forbidden files exist in the history:" >&2
  echo "$history" >&2
  status=1
fi

for rule in '*.gramps' '*.gpkg' '*.ged' '*.db'; do
  if ! git check-ignore -q --no-index "sample${rule#\*}"; then
    echo "The .gitignore file must ignore $rule" >&2
    status=1
  fi
done

if [ "$status" -eq 0 ]; then
  echo "No family data files found."
fi
exit "$status"
