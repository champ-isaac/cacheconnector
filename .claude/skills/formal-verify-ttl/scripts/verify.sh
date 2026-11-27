#!/usr/bin/env bash
# Formal TTL check for cacheconnector.
#   verify.sh check     run both checks against the real code (about 20 s)
#   verify.sh mutants   break the code on purpose, once per mutant, and confirm each break is caught
#   verify.sh fuzz [d]  extra random search on memcached expiration, default 30s
#   verify.sh drift     list covered files that changed since the sync commit
# The repo is never edited. Checks and mutants reach the packages through `go test -overlay`.
set -u
SKILL="$(cd "$(dirname "$0")/.." && pwd)"
REPO="$(cd "$SKILL/../../.." && pwd)"
SYNC="$(sed -n 's/^sync-commit: //p' "$SKILL/SKILL.md" | head -1)"
COVERED="memcached/client.go inmemory/client.go"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$REPO"

overlay() { # overlay <out.json> [<repo-relative real file> <replacement>]
  out=$1; shift
  {
    printf '{"Replace":{"%s":"%s","%s":"%s"' \
      "$REPO/memcached/zz_formal_test.go" "$SKILL/checks/memcached/expiration_test.go" \
      "$REPO/inmemory/zz_formal_test.go" "$SKILL/checks/inmemory/lifetime_test.go"
    [ $# -eq 2 ] && printf ',"%s":"%s"' "$REPO/$1" "$2"
    printf '}}'
  } > "$out"
}

run() { shift_pkgs=${2:-./memcached ./inmemory}; go test -count=1 -overlay "$1" -run 'TestFormal' $shift_pkgs; }

drift() {
  if [ -z "$SYNC" ]; then echo "no sync-commit in SKILL.md"; return 1; fi
  changed=$(git diff --name-only "$SYNC" -- $COVERED)
  if [ -n "$changed" ]; then
    echo "DRIFT: covered files changed since $SYNC, re-read the coverage record before trusting a pass:"
    echo "$changed"
    return 1
  fi
  echo "no drift: covered files are unchanged since $SYNC"
}

mutate() { # mutate <name> <file> <perl -0 expression>; prints whether the checks caught it
  name=$1 file=$2 expr=$3
  cp "$file" "$WORK/orig.go"
  perl -0pe "$expr" "$file" > "$WORK/mutant.go"
  if cmp -s "$file" "$WORK/mutant.go"; then echo "STALE  $name: the pattern no longer matches $file, update scripts/verify.sh"; return 2; fi
  overlay "$WORK/m.json" "$file" "$WORK/mutant.go"
  if run "$WORK/m.json" "./$(dirname "$file")" > "$WORK/m.out" 2>&1; then echo "MISSED $name"; return 1; fi
  if grep -q 'build failed\|setup failed' "$WORK/m.out"; then echo "BROKEN $name: mutant did not compile"; return 2; fi
  echo "CAUGHT $name: $(grep -E 'zz_formal_test\.go:[0-9]+:' "$WORK/m.out" | grep -v 'checked ' | head -1 | sed 's/^ *zz_formal_test.go:[0-9]*: //' | cut -c1-160)"
}

case "${1:-}" in
  check)
    drift || true
    overlay "$WORK/o.json"
    run "$WORK/o.json" ;;
  mutants)
    bad=0
    mutate "memcached: sub-second ttl no longer rounds up"      memcached/client.go 's/\t\tsecs\+\+/\t\tsecs += 0/'                       || bad=1
    mutate "memcached: 30-day switch moved to 60 days"          memcached/client.go 's/ttl > maxRelativeTTL/ttl > 2*maxRelativeTTL/' || bad=1
    mutate "memcached: int32 ceiling removed"                   memcached/client.go 's/secs > math\.MaxInt32/secs > math.MaxInt64/' || bad=1
    mutate "memcached: absolute timestamp forgets the clock"    memcached/client.go 's/secs \+= now\.Unix\(\)/secs += 0/'          || bad=1
    mutate "memcached: non-positive ttl becomes 1 second"       memcached/client.go 's/(if ttl <= 0 \{\n\t\treturn )0/${1}1/'      || bad=1
    mutate "inmemory: entry lives one tick past its deadline"   inmemory/client.go  's/!now\.Before\(e\.expires\)/now.After(e.expires)/' || bad=1
    mutate "inmemory: sweep deletes live entries"               inmemory/client.go  's/if e\.expired\(now\) \{/if e.expired(now) || !e.expires.IsZero() {/' || bad=1
    mutate "inmemory: negative ttl expires at once"             inmemory/client.go  's/if ttl > 0 \{/if ttl != 0 {/'               || bad=1
    mutate "inmemory: sweep never deletes"                      inmemory/client.go  's/if e\.expired\(now\) \{/if e.expired(now) \&\& false {/'        || bad=1
    exit $bad ;;
  fuzz)
    echo "fuzzing needs the check file inside the package, copying it for this run only"
    cp "$SKILL/checks/memcached/expiration_test.go" memcached/zz_formal_test.go
    trap 'rm -f "$REPO/memcached/zz_formal_test.go"; rm -rf "$WORK"' EXIT
    go test -run '^$' -fuzz FuzzFormalExpiration -fuzztime "${2:-30s}" ./memcached ;;
  drift) drift ;;
  *) sed -n 2,7p "$0"; exit 2 ;;
esac
