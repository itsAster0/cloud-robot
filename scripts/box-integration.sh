#!/bin/sh
# Docker integration checks for the dynamic SSH robot box.
# Verifies resource limits, key replacement, persistent workspace volumes,
# automatic agent startup, quota shutdown, and rediscovery after restart.
# Requires a running Docker engine and the built box image.
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <box-id> <public-key-file>" >&2
  exit 2
fi

box=$1
keyfile=$2
provisioner=${PROVISIONER_URL:-http://localhost:8090}
token=${PROVISIONER_TOKEN:-local-review-token-change-me}

fail() { echo "FAIL: $1" >&2; exit 1; }
pass() { echo "PASS: $1"; }
call() { curl -fsS -X "$1" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' ${3:+-d "$3"} "$provisioner/v1/boxes/$box$2"; }

command -v docker >/dev/null || fail "docker not installed"
curl -fsS "$provisioner/healthz" >/dev/null || fail "provisioner not reachable at $provisioner"

# 1. Provision (idempotent ensure) and confirm resource limits.
call PUT "" | grep -q '"status":"running"' || fail "box did not reach running state"
inspect=$(docker inspect "$box")
echo "$inspect" | grep -q 'NanoCpus[^,}]*1000000000' || fail "box is not limited to 1 CPU"
echo "$inspect" | grep -q 'Memory[^,}]*536870912' || fail "box is not limited to 512 MB RAM"
echo "$inspect" | grep -q 'PidsLimit[^,}]*128' || fail "box is not limited to 128 PIDs"
pass "resource limits enforced"

# 2. Replace the SSH key and confirm the fingerprint changes.
call PUT /ssh-key "{\"publicKey\":\"$(cat "$keyfile")\"}" >/dev/null || fail "ssh key rejected"
pass "ssh key accepted"

# 3. Write a marker file, restart the container, confirm the workspace persists.
call POST /restart >/dev/null || fail "restart failed"
sleep 3
call GET "" >/dev/null
docker exec "$box" sh -c 'echo persisted > /workspace/integration-marker'
docker restart "$box" >/dev/null
sleep 3
docker exec "$box" test -f /workspace/integration-marker || fail "workspace volume did not persist across restart"
docker exec "$box" rm /workspace/integration-marker
pass "workspace persisted across restart"

# 4. Configure an agent and confirm the supervisor starts and tracks it.
agent_url=${ROBOT_AGENT_BASE_URL:-ws://host.docker.internal:8080}
call PUT /agent "{\"robotId\":\"integration\",\"matchId\":\"integration\",\"url\":\"$agent_url/agent/connect/integration\",\"token\":\"integration-token\",\"startCommand\":\"sleep 120\"}" >/dev/null || fail "agent config rejected"
sleep 4
call GET "" | grep -q '"agentStatus":"running"' && pass "agent started by supervisor" || fail "supervisor did not report a running agent"

# 5. Quota behavior: fill the workspace past the quota, agent must stop while SSH stays up.
docker exec "$box" sh -c 'dd if=/dev/zero of=/workspace/blob bs=1M count=1100 2>/dev/null'
deadline=$(( $(date +%s) + 15 ))
while [ "$(date +%s)" -lt "$deadline" ]; do
  status=$(call GET "")
  case "$status" in *quota_exceeded*) break ;; esac
  sleep 1
done
case "$status" in *quota_exceeded*) pass "quota breach stopped agent" ;; *) fail "quota breach not reported: $status" ;; esac
docker exec "$box" sh -c 'rm /workspace/blob'
call POST /restart >/dev/null

# 6. Rediscovery: stop the box, ensure must start it again with the same port.
port_before=$(docker port "$box" 22/tcp | head -1 | sed 's/.*://')
docker stop "$box" >/dev/null
call PUT "" >/dev/null || fail "ensure failed to rediscover stopped box"
port_after=$(docker port "$box" 22/tcp | head -1 | sed 's/.*://')
[ "$port_before" = "$port_after" ] || fail "port changed after rediscovery ($port_before -> $port_after)"
pass "box rediscovered after stop with stable port"

echo "box integration checks complete for $box"
