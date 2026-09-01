#!/bin/sh
set -eu

if [ "$#" -lt 1 ]; then
  echo "usage: $0 user@host [ssh-port]" >&2
  exit 2
fi

target=$1
port=${2:-22}

port_start=${SSH_PORT_START:-${SSH_PORT_MIN:-22000}}
port_count=${SSH_PORT_COUNT:-1000}
port_end=${SSH_PORT_END:-$((port_start + port_count - 1))}

case "$port_start:$port_end" in
  *[!0-9:]*|:*) echo "SSH box port range must contain positive integers" >&2; exit 2 ;;
esac
if [ "$port_start" -gt "$port_end" ] || { [ "$port_start" -le 22 ] && [ "$port_end" -ge 22 ]; }; then
  echo "invalid SSH box port range: $port_start-$port_end" >&2
  exit 2
fi

echo "SSH_BOX_PORT_RANGE=$port_start-$port_end"
echo "UFW_COMMAND=sudo ufw allow ${port_start}:${port_end}/tcp"
echo "FIREWALLD_COMMAND=sudo firewall-cmd --permanent --add-port=${port_start}-${port_end}/tcp"

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
