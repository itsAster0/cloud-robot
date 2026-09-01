# VPS Deployment

## Inspect host

Use public-key authentication. Keep provider console access until SSH changes are verified.

```sh
ssh-keygen -t ed25519 -a 64 -f ~/.ssh/robot_arena_vps
ssh-copy-id -i ~/.ssh/robot_arena_vps.pub deploy@example.com
mise run vps:doctor -- deploy@example.com 22
```

Doctor command is read-only. It reports OS, CPU, memory, KVM, Docker, Compose, and disk.

## Review host

- Current 64-bit Linux distribution
- Docker Engine and Compose plugin
- 4 CPU cores and 8 GiB RAM
- HTTPS on ports 80 and 443
- SSH box port range restricted by firewall
- Persistent Docker volume storage

Set `SSH_PUBLIC_HOST` to public DNS name. Change `PROVISIONER_TOKEN`. Keep provisioner port private. Main API may call provisioner only through internal network.

Use Linux filesystem project quotas or supported Docker `storage-opt` enforcement for hard 1 GB box limits. Local usage monitoring is not enough for public deployment.

## Production boundary

Do not expose current boxes to untrusted users. Docker socket gives provisioner host-level control. Public competition requires Firecracker/KVM isolation, egress filtering, metadata-service blocking, per-box filesystems, credential rotation, image signing, audit logs, teardown verification, and adversarial review.

Use real S3, DynamoDB, and SQS by changing endpoint and credentials. Floci remains local demo infrastructure.
