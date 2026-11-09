#!/usr/bin/env bash
# Starts the real binary with invented data and checks the main security
# promises over HTTP.
set -euo pipefail

bin=${1:-./heirfold}
dir=$(mktemp -d)
pid=""
cleanup() {
  if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; fi
  rm -rf "$dir"
}
trap cleanup EXIT

port=18473
base="http://127.0.0.1:$port"
fail() {
  echo "FAILED: $1" >&2
  if [ -f "$dir/server.log" ]; then tail -20 "$dir/server.log" >&2; fi
  exit 1
}

"$bin" fixture --locale en --out "$dir/demo.gramps"
"$bin" import --data "$dir/data" "$dir/demo.gramps"
# The accounts get the numbers 1 to 4 in this order.
"$bin" user-add --data "$dir/data" --email smokeadmin@example.org --role admin
"$bin" user-add --data "$dir/data" --email smokeguest@example.org --role guest
"$bin" user-add --data "$dir/data" --email smokemember@example.org --role member
"$bin" user-add --data "$dir/data" --email smokeeditor@example.org --role editor

"$bin" serve --data "$dir/data" --listen "127.0.0.1:$port" >"$dir/server.log" 2>&1 &
pid=$!
for _ in $(seq 1 50); do
  if curl -fsS "$base/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.2
done
curl -fsS "$base/healthz" >/dev/null || fail "the server did not start"

headers=$(curl -fsS -D - -o "$dir/index.html" "$base/")
for expected in "default-src 'none'" "script-src 'self'" "require-trusted-types-for 'script'" "X-Content-Type-Options: nosniff" "Referrer-Policy: no-referrer"; do
  grep -qi "$expected" <<<"$headers" || fail "the header $expected is missing"
done
if grep -qi "unsafe-inline\|unsafe-eval" <<<"$headers"; then fail "the CSP allows unsafe sources"; fi
if grep -Eqi '<script[^>]+src="https?:|<link[^>]+href="https?:' "$dir/index.html"; then fail "the page loads a third-party file"; fi
if grep -Eqi '<script>|<style>| style="' "$dir/index.html"; then fail "the page has inline code"; fi

code=$(curl -s -o /dev/null -w '%{http_code}' "$base/api/v1/tree")
[ "$code" = "401" ] || fail "the tree is open without a session: $code"

# The server has no mail settings, so it writes each sign-in code to its log.
# login NAME NUMBER signs the account in with that code.
login() {
  curl -fsS -c "$dir/$1.jar" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$1@example.org\",\"lang\":\"en\"}" "$base/api/v1/auth/code" >/dev/null
  local code=""
  for _ in $(seq 1 25); do
    code=$(grep "user=$2 " "$dir/server.log" | grep -o 'code=[0-9]*' | tail -1 | cut -d= -f2 || true)
    if [ -n "$code" ]; then break; fi
    sleep 0.2
  done
  [ -n "$code" ] || fail "no sign-in code for $1"
  curl -fsS -b "$dir/$1.jar" -c "$dir/$1.jar" -H 'Content-Type: application/json' \
    -d "{\"code\":\"$code\"}" "$base/api/v1/auth/verify" >/dev/null
}
login smokeadmin 1
login smokeguest 2
login smokemember 3
login smokeeditor 4

code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
  -d '{"email":"nobody@example.org","lang":"en"}' "$base/api/v1/auth/code")
[ "$code" = "200" ] || fail "an unknown address gets another answer than a known one: $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -c "$dir/wrong.jar" -b "$dir/smokeguest.jar" -H 'Content-Type: application/json' \
  -d '{"code":"00000000"}' "$base/api/v1/auth/verify")
[ "$code" = "401" ] || fail "a wrong sign-in code passed: $code"

guest=$(curl -fsS -b "$dir/smokeguest.jar" "$base/api/v1/tree" | jq '[.people[] | select(.living == true)] | length')
[ "$guest" = "0" ] || fail "a guest sees $guest living people"
member=$(curl -fsS -b "$dir/smokemember.jar" "$base/api/v1/tree" | jq '[.people[] | select(.living == true)] | length')
[ "$member" -gt 0 ] || fail "a member sees no living people"

code=$(curl -s -o /dev/null -w '%{http_code}' -b "$dir/smokeguest.jar" "$base/api/v1/export.gramps")
[ "$code" = "403" ] || fail "a guest can export: $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$dir/smokeadmin.jar" "$base/api/v1/export.gramps")
[ "$code" = "200" ] || fail "an admin cannot export: $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$dir/smokemember.jar" -X POST -H 'Sec-Fetch-Site: cross-site' "$base/api/v1/auth/logout")
[ "$code" = "403" ] || fail "a cross-site write passed: $code"

# Editing: an editor adds a person, a member cannot, and a guest never sees
# the new person, who counts as living.
person='{"person":{"gender":"F","private":false,"names":[{"index":-1,"type":"Birth Name","given":"Smoke","surname":"Testperson","patronymic":"","suffix":"","title":"","nick":"","call":""}]},"relation":null}'
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$dir/smokemember.jar" -H 'Content-Type: application/json' -d "$person" "$base/api/v1/edit/people")
[ "$code" = "403" ] || fail "a member can edit the tree: $code"
created=$(curl -fsS -b "$dir/smokeeditor.jar" -H 'Content-Type: application/json' -d "$person" "$base/api/v1/edit/people" | jq -r '.created')
[ -n "$created" ] && [ "$created" != "null" ] || fail "an editor cannot add a person"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$dir/smokemember.jar" "$base/api/v1/people/$created")
[ "$code" = "200" ] || fail "a member does not see the new person: $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$dir/smokeguest.jar" "$base/api/v1/people/$created")
[ "$code" = "404" ] || fail "a guest sees a new living person: $code"
change=$(curl -fsS -b "$dir/smokeeditor.jar" "$base/api/v1/history" | jq -r '.entries[0].id')
curl -fsS -b "$dir/smokeeditor.jar" -X POST -H 'Content-Type: application/json' -d '{}' "$base/api/v1/history/$change/undo" >/dev/null || fail "the undo failed"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$dir/smokemember.jar" "$base/api/v1/people/$created")
[ "$code" = "404" ] || fail "the person is still there after the undo: $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -b "$dir/smokeeditor.jar" "$base/api/v1/audit")
[ "$code" = "403" ] || fail "an editor reads the audit log: $code"

if grep -Eq 'example\.org|Testperson' "$dir/server.log"; then fail "the log holds an address or a name"; fi
echo "Smoke test passed."
