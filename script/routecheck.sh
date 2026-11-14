#!/bin/sh
# idl-check helper (I10): every route registered in router.go must appear
# in the handwritten whitelist, and vice versa. Generated routes live in
# the IDL and are checked by the make idl diff instead.
set -e
reg=$(mktemp)
wl=$(mktemp)
trap 'rm -f "$reg" "$wl"' EXIT
grep -oE 'r\.(GET|POST)\("[^"]+"' router.go | sed 's/r\.//;s/("/ /;s/"$//' | tr -s ' ' > "$reg"
grep -vE '^#|^$' idl/handwritten-routes.txt | tr -s ' ' > "$wl"
fail=0
while read -r line; do
  if ! grep -qxF "$line" "$wl"; then
    echo "routecheck: $line is registered but not in idl/handwritten-routes.txt"
    fail=1
  fi
done < "$reg"
while read -r line; do
  if ! grep -qxF "$line" "$reg"; then
    echo "routecheck: $line is whitelisted but not registered in router.go"
    fail=1
  fi
done < "$wl"
exit $fail
