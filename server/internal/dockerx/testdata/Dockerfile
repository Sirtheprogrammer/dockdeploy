# An sshd host with the Docker socket mounted, standing in for a managed
# server. It exists so the SSH-to-Docker-socket tunnel can be tested without a
# real VPS.
FROM alpine:3.21

# openssh-client, not just the server: cloning over SSH with a deploy key
# needs the ssh binary, and openssh-server alone does not provide it.
RUN apk add --no-cache openssh-server openssh-client docker-cli docker-cli-compose git \
 && ssh-keygen -A \
 && mkdir -p /root/.ssh && chmod 700 /root/.ssh

COPY authorized_keys /root/.ssh/authorized_keys

# OpenSSH honours the FIRST occurrence of a keyword, so these are edited in
# place. Appending an override at the end of the file silently does nothing --
# which is exactly the trap this fixture originally fell into.
#
# direct-streamlocal, the channel type used to reach the Docker socket, is
# gated on AllowTcpForwarding despite the name.
RUN chmod 600 /root/.ssh/authorized_keys \
 && sed -i 's/^AllowTcpForwarding no/AllowTcpForwarding yes/' /etc/ssh/sshd_config \
 && sed -i 's/^#*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config \
 && printf 'AllowStreamLocalForwarding yes\n' >> /etc/ssh/sshd_config \
 && sshd -t

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
