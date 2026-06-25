# NFS-Ganesha userspace NFSv4 server for the go-nfs-client integration harness.
#
# Built from Ubuntu's packaged nfs-ganesha so it works on both amd64 and arm64
# (Apple Silicon) without depending on a third-party prebuilt image. Ganesha is
# a userspace server; combined with go-nfs-client's userspace TCP client, no
# kernel NFS modules or host-side mounts are required.

FROM ubuntu:24.04

RUN apt-get update \
	&& DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
		nfs-ganesha \
		nfs-ganesha-vfs \
		dbus \
		netbase \
	&& rm -rf /var/lib/apt/lists/*

# The exported directory. The compose file mounts a named volume here; we create
# it so the image also works standalone.
RUN mkdir -p /export && chmod 0777 /export

# Ganesha needs these runtime dirs.
RUN mkdir -p /var/run/ganesha /var/lib/nfs/ganesha

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 2049/tcp

ENTRYPOINT ["/entrypoint.sh"]
