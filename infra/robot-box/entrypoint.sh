#!/bin/sh
set -eu

# Control dir is traversal-only (0711): the developer user must be able to
# read authorized_keys because sshd reads it after dropping privileges, but
# cannot list or open the root-private agent files that hold robot tokens.
install -d -m 711 /var/lib/robot-box
touch /var/lib/robot-box/authorized_keys
chown root:root /var/lib/robot-box/authorized_keys
chmod 644 /var/lib/robot-box/authorized_keys
# Debian useradd locks the account; sshd with UsePAM no refuses locked
# accounts. Unlock with a no-password marker so public key login works. This
# also heals boxes provisioned from older images.
usermod -p '*' developer
ssh-keygen -A
/usr/sbin/sshd
exec /usr/local/bin/robot-box-supervisor daemon
