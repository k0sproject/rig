#!/usr/bin/env sh
#shellcheck disable=SC3037,SC3043

abs() (
  if [ -d "$1" ]; then
    cd "$1" || return 1
    pwd
  elif [ -e "$1" ]; then
    if [ ! "${1%/*}" = "$1" ]; then
      cd "${1%/*}" || return 1
    fi
    echo "$(pwd)/${1##*/}"
  else
    return 1
  fi
)

statjson() {
  local path="$1"
  local embed
  if [ "$2" = "true" ]; then
    embed=1
  fi

  if [ "$path" = "" ]; then
    throw "empty path"
  fi
  if stat --help 2>&1 | grep -q -- 'GNU\|BusyBox'; then
    # GNU/BusyBox stat
    file_info=$(stat -c '0%a %s %Y' -- "$path" 2> /dev/null)
  else
    # BSD stat
    file_info=$(stat -f '%Mp%Lp %z %m' "$path" 2> /dev/null)
  fi
  read -r unix_mode size mod_time <<EOF
$file_info
EOF

  unix_mode=$(printf "%d" "$unix_mode")

  if [ -d "$path" ]; then
    is_dir=true
  else
    is_dir=false
  fi
  if [ -z "$embed" ]; then
    echo -n "{\"stat\":{\"size\":$size,\"unixMode\":$unix_mode,\"modTime\":$mod_time,\"isDir\":$is_dir,\"name\":\"$path\"}}"
  else
    echo -n "{\"size\":$size,\"unixMode\":$unix_mode,\"modTime\":$mod_time,\"isDir\":$is_dir,\"name\":\"$path\"}"
  fi
}

throw() {
  echo -n "{\"error\":\"$*\"}"
  exit 1
}

main() {
  local cmd="$1"
  local path

  if [ "$2" != "" ]; then
    path=$(abs "$2" 2> /dev/null || echo "$2")
  fi


  case "${cmd}" in
    "stat")
      if [ ! -e "$path" ]; then
        throw "file not found"
      fi
      statjson "$path"
      ;;
    "sum")
      if [ ! -f "$path" ]; then
        throw "file not found"
      fi
      sum=$(sha256sum -b "$path" | awk '{print $1}')
      if [ -z "$sum" ]; then
        throw "failed to calculate checksum"
      fi
      echo -n "{\"sum\":{\"sha256\":\"$sum\"}}"
      ;;
    "dir")
      if [ ! -d "$path" ]; then
        throw "directory not found"
      fi
      echo -n "{\"dir\":["
      first=true
      for file in "$path"/.* "$path"/*; do
        case "$file" in "$path"/. | "$path"/..) continue ;; esac
        if [ "$first" = true ]; then
          first=false
        else
          echo -n ","
        fi
        statjson "$file" true
      done
      echo -n "]}"
      ;;
    "touch")
      local perm="$3"
      touch "$path" && echo -n "{}"
      chmod "$perm" "$path"
      ;;
    "truncate")
      local pos="$3"
      truncate -s "$pos" "$path" && echo -n "{}"
      ;;
    *)
      # write the response to standard output
      throw "invalid command: ${cmd}"
      ;;
  esac
}

main "$@"
