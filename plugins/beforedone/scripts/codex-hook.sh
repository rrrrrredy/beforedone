#!/usr/bin/env sh
set -eu
export BEFOREDONE_PLUGIN_VERSION=1.0.1

beforedone_path=""
current_directory=$(pwd -P)
worktree_root=""
cursor=$current_directory
while :; do
  if [ -e "$cursor/.git" ]; then
    # Keep the outermost marker so a nested .git cannot whitelist another
    # executable that still lives inside the surrounding worktree.
    worktree_root=$cursor
  fi
  [ "$cursor" = "/" ] && break
  cursor=$(dirname "$cursor")
done

is_in_worktree() {
  [ -n "$worktree_root" ] || return 1
  case "$1" in
    "$worktree_root"|"$worktree_root"/*) return 0 ;;
    *) return 1 ;;
  esac
}

old_ifs=$IFS
IFS=:
for path_entry in ${PATH-}; do
  IFS=$old_ifs
  case "$path_entry" in
    ""|"."|"$PWD") ;;
    /*)
      resolved_directory=$(cd "$path_entry" 2>/dev/null && pwd -P) || resolved_directory=""
      candidate="$resolved_directory/beforedone"
      if [ -n "$resolved_directory" ] && ! is_in_worktree "$resolved_directory" && [ -x "$candidate" ] && [ ! -d "$candidate" ]; then
        link_hops=0
        while [ -L "$candidate" ]; do
          link_hops=$((link_hops + 1))
          [ "$link_hops" -le 40 ] || { candidate=""; break; }
          target=$(readlink "$candidate") || { candidate=""; break; }
          case "$target" in
            /*) candidate=$target ;;
            *) candidate=$(dirname "$candidate")/$target ;;
          esac
          candidate_directory=$(cd "$(dirname "$candidate")" 2>/dev/null && pwd -P) || { candidate=""; break; }
          candidate="$candidate_directory/$(basename "$candidate")"
        done
        [ -n "$candidate" ] || continue
        is_in_worktree "$candidate" && continue
        beforedone_path="$candidate"
        break
      fi
      ;;
  esac
  IFS=:
done
IFS=$old_ifs

if [ -z "$beforedone_path" ]; then
  printf '%s\n' '{"systemMessage":"BeforeDone CLI is required by the plugin but was not found in an absolute PATH directory. Install it with: go install github.com/rrrrrredy/beforedone/cmd/beforedone@latest — or use https://github.com/rrrrrredy/beforedone/releases/latest"}'
  exit 0
fi

exec "$beforedone_path" hook codex
