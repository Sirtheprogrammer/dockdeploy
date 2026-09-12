#!/bin/sh
# Starts an sshd container with the Docker socket mounted, so the tunnel tests
# have something real to connect to.
#
#   sh fixture.sh up      start it and print the env to export
#   sh fixture.sh down    remove it
#
# The generated keypair is throwaway and lives in this directory, which is
# gitignored.
set -eu

NAME=dockdeploy-sshd-fixture
IMAGE=dockdeploy-sshd-fixture
PORT=${DOCKDEPLOY_SSH_PORT:-2222}
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

case "${1:-up}" in
up)
    if [ ! -f "$DIR/id_ed25519" ]; then
        ssh-keygen -t ed25519 -N '' -C dockdeploy-fixture -f "$DIR/id_ed25519" >/dev/null
    fi
    cp "$DIR/id_ed25519.pub" "$DIR/authorized_keys"

    docker build -q -t "$IMAGE" "$DIR" >/dev/null
    docker rm -f "$NAME" >/dev/null 2>&1 || true

    # MSYS_NO_PATHCONV stops Git Bash on Windows rewriting the socket path.
    MSYS_NO_PATHCONV=1 docker run -d --name "$NAME" \
        -p "127.0.0.1:$PORT:22" \
        -v /var/run/docker.sock:/var/run/docker.sock \
        "$IMAGE" >/dev/null

    # Wait for sshd rather than guessing at a sleep duration.
    i=0
    while [ "$i" -lt 30 ]; do
        if docker exec "$NAME" pgrep sshd >/dev/null 2>&1; then break; fi
        i=$((i + 1))
        sleep 1
    done

    cat <<ENV
Fixture running. Run the tunnel tests with:

  DOCKDEPLOY_SSH_HOST=127.0.0.1 \
  DOCKDEPLOY_SSH_PORT=$PORT \
  DOCKDEPLOY_SSH_USER=root \
  DOCKDEPLOY_SSH_KEY=$DIR/id_ed25519 \
  go test ./internal/dockerx/ -run Tunnel -v
ENV
    ;;
down)
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    echo "Fixture removed."
    ;;
*)
    echo "usage: $0 [up|down]" >&2
    exit 2
    ;;
esac
