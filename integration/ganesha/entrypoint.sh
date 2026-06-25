#!/usr/bin/env bash
# Entrypoint for the integration NFS-Ganesha server.
#
# Ganesha needs D-Bus running for its management interface and writable runtime
# directories. We start dbus, then run ganesha.nfsd in the foreground so the
# container's lifecycle follows the server process.
set -euo pipefail

# Ensure the export dir exists and is world-writable for the AUTH_SYS tests
# (No_Root_Squash is configured, but the tests may run as arbitrary uid).
mkdir -p /export
chmod 0777 /export

# D-Bus is required by ganesha for SetConf/stats; start it best-effort.
mkdir -p /run/dbus
rm -f /run/dbus/pid
dbus-daemon --system --fork || true

CONF="${GANESHA_CONFIG:-/etc/ganesha/ganesha.conf}"

echo "Starting ganesha.nfsd with config ${CONF}"
# -F: foreground, -L /dev/stdout: log to stdout, -p: pid file.
exec ganesha.nfsd -F -L /dev/stdout -p /var/run/ganesha/ganesha.pid -f "${CONF}"
