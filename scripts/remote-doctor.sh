#!/bin/sh
set -eu

if [ "$#" -lt 1 ]; then
  echo "usage: $0 user@host [ssh-port]" >&2
  exit 2
fi

target=$1
port=${2:-22}

ssh -p "$port" -o BatchMode=yes -o ConnectTimeout=8 "$target" '
  set -eu
  echo "HOST=$(hostname)"
  echo "OS=$(uname -srm)"
  echo "CPU=$(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc)"
  echo "MEMORY_KB=$(awk '\''/MemTotal/ {print $2}'\'' /proc/meminfo 2>/dev/null || echo unknown)"
  echo "KVM=$(test -r /dev/kvm -a -w /dev/kvm && echo available || echo unavailable)"
  echo "DOCKER=$(docker --version 2>/dev/null || echo missing)"
  echo "COMPOSE=$(docker compose version 2>/dev/null || echo missing)"
  echo "DISK=$(df -h . | tail -1)"
'

