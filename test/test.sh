#!/usr/bin/env bash

RET=0
set -e

color_echo() {
  echo -e "\033[1;31m$*\033[0m"
}

ssh_port() {
	bootloose show "$1" -o json|grep hostPort|grep -oE "[0-9]+"
}

sanity_check() {
  color_echo "- Testing bootloose machine connection"
  make create-host
  echo "* bootloose status"
  bootloose status
  echo "* Docker ps"
  docker ps
  echo "* SSH port: $(ssh_port node0)"
  echo "* Testing stock ssh"
  retry ssh -vvv -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i .ssh/id_ed25519 -p "$(ssh_port node0)" root@127.0.0.1 echo "test-conn" || return $?
  set +e
  echo "* Testing bootloose ssh"
  bootloose ssh root@node0 echo test-conn | grep -q test-conn
  exit_code=$?
  set -e
  make clean
  RET=$exit_code
}

rig_test_key_from_path() {
  color_echo "- Testing regular keypath and host functions"
  make create-host
  mv .ssh/id_ed25519 .ssh/identity2
  set +e
  go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath .ssh/identity2 
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_agent_with_public_key() {
  color_echo "- Testing connection using agent and providing a path to public key"
  make create-host
  eval "$(ssh-agent -s)"
  ssh-add .ssh/id_ed25519
  rm -f .ssh/id_ed25519
  set +e
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath .ssh/id_ed25519.pub -connect
  exit_code=$?
  set -e
  kill "$SSH_AGENT_PID"
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  RET=$exit_code
}

rig_test_agent_with_private_key() {
  color_echo "- Testing connection using agent and providing a path to protected private key"
  make create-host KEY_PASSPHRASE=testPhrase
  eval "$(ssh-agent -s)"
  expect -c '
    spawn ssh-add .ssh/id_ed25519
    expect "?:"
    send "testPhrase\n"
    expect eof"
  '
  set +e
  # path points to a private key, rig should try to look for the .pub for it 
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath .ssh/id_ed25519 -connect
  exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  RET=$exit_code
}

rig_test_agent() {
  color_echo "- Testing connection using any key from agent (empty keypath)"
  make create-host
  eval "$(ssh-agent -s)"
  ssh-add .ssh/id_ed25519
  rm -f .ssh/id_ed25519
  set +e
  ssh-add -l
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath "" -connect
  exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  RET=$exit_code
}

rig_test_agent_and_invalid_key() {
  color_echo "- Testing connection using any key from agent (empty keypath) when there is an invalid key in the agent and filesystem"
  make create-host
  eval "$(ssh-agent -s)"
  ssh-keygen -f .ssh/id_rsa -N ""
  ssh-add .ssh/id_rsa
  ssh-add .ssh/id_ed25519
  rm -f .ssh/id_ed25519
  set +e
  ssh-add -l
  HOME=$(pwd) SSH_AUTH_SOCK=$SSH_AUTH_SOCK go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-keypath "" -connect
  exit_code=$?
  set -e
  kill $SSH_AGENT_PID
  export SSH_AGENT_PID=
  export SSH_AUTH_SOCK=
  rm -f .ssh/id_rsa
  RET=$exit_code
}

rig_test_ssh_config() {
  color_echo "- Testing getting identity path from ssh config"
  make create-host
  mv .ssh/id_ed25519 .ssh/identity2
  echo "Host 127.0.0.1" > .ssh/config
  echo "  IdentityFile $(pwd)/.ssh/identity2" >> .ssh/config
  chmod 0600 .ssh/config
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host 127.0.0.1 -port "$(ssh_port node0)" -user root -connect
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_ssh_config_strict() {
  color_echo "- Testing StrictHostkeyChecking=yes in ssh config"
  make create-host
  port="$(ssh_port node0)"
  echo "Host testhost" > .ssh/config
  echo "  User root" >> .ssh/config
  echo "  HostName 127.0.0.1" >> .ssh/config
  echo "  Port ${port}" >> .ssh/config
  echo "  IdentityFile $(pwd)/.ssh/id_ed25519" >> .ssh/config
  echo "  UserKnownHostsFile $(pwd)/.ssh/known" >> .ssh/config
  echo "  StrictHostKeyChecking yes" >> .ssh/config
  cat .ssh/config
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host testhost -connect
  exit_code=$?
  set -e
  if [ $exit_code -ne 0 ]; then
    echo "  * Failed first checkpoint"
    RET=1
    return
  fi
  echo "  * Passed first checkpoint"
  cat .ssh/known
  # modify the known hosts file to make it mismatch
  echo "[127.0.0.1]:$port ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBBgejI9UJnRY/i4HNM/os57oFcRjE77gEbVfUkuGr5NRh3N7XxUnnBKdzrAiQNPttUjKmUm92BN7nCUxbwsoSPw=" > .ssh/known
  echo "[127.0.0.1]:$port ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBGZKwBdFeIPlDWe7otNy4E2Im8+GnQtsukJ5dIuzDGb" >> .ssh/known
  cat .ssh/known
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host testhost -connect
  exit_code=$?
  set -e

  if [ $exit_code -eq 0 ]; then
    echo "  * Failed second checkpoint"
    # success is a failure
    RET=1
    return
  fi
  echo "  * Passed second checkpoint"
}

rig_test_ssh_config_no_strict() {
  color_echo "- Testing StrictHostkeyChecking=no in ssh config"
  make create-host
  port="$(ssh_port node0)"
  echo "Host testhost" > .ssh/config
  echo "  User root" >> .ssh/config
  echo "  HostName 127.0.0.1" >> .ssh/config
  echo "  Port ${port}" >> .ssh/config
  echo "  UserKnownHostsFile $(pwd)/.ssh/known" >> .ssh/config
  echo "  StrictHostKeyChecking no" >> .ssh/config
  cat .ssh/config
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host testhost -connect
  exit_code=$?
  set -e
  if [ $? -ne 0 ]; then
    RET=1
    return
  fi
  # modify the known hosts file to make it mismatch
  echo "[127.0.0.1]:$port ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBBgejI9UJnRY/i4HNM/os57oFcRjE77gEbVfUkuGr5NRh3N7XxUnnBKdzrAiQNPttUjKmUm92BN7nCUxbwsoSPw=" > .ssh/known
  echo "[127.0.0.1]:$port ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBGZKwBdFeIPlDWe7otNy4E2Im8+GnQtsukJ5dIuzDGb" >> .ssh/known
  set +e
  HOME=$(pwd) go test -v ./ -args -ssh-configpath .ssh/config -host testhost -connect -trace
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_key_from_memory() {
  color_echo "- Testing connecting using a key from string"
  make create-host
  mv .ssh/id_ed25519 .ssh/identity2
  set +e
  go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -ssh-private-key "$(cat .ssh/identity2)" -connect
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_key_from_default_location() {
  color_echo "- Testing keypath from default location"
  make create-host
  set +e
  HOME=$(pwd) go test -v ./ -args -host 127.0.0.1 -port "$(ssh_port node0)" -user root -connect
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_regular_user() {
  color_echo "- Testing regular user"
  run_suite_as_rigtest_user
}

rig_test_regular_user_fish_login_shell() {
  color_echo "- Testing regular user with a fish login shell"
  run_suite_as_rigtest_user fish
}

# run_suite_as_rigtest_user provisions an unprivileged rigtest-user with
# passwordless privilege escalation on node0 and runs the suite as that user.
# $1 is an optional package name for a login shell to install and assign to the
# user; sshd runs every command through that shell, so a non-POSIX one puts the
# shell imposition of cmd.Executor.SetShell and the sudo decorators to the test.
run_suite_as_rigtest_user() {
  local shellPkg="${1:-}"
  make create-host
  local sshPort
  sshPort=$(ssh_port node0)

  set -- -T -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i .ssh/id_ed25519 -p "$sshPort"
  retry ssh "$@" root@127.0.0.1 true || {
    RET=$?
    color_echo failed to SSH into machine >&2
    return 0
  }

  # shellcheck disable=SC2029 # the package name is meant to expand on this side
  ssh "$@" root@127.0.0.1 "LOGIN_SHELL_PKG='$shellPkg' sh -euxC -" <<'EOF'
    # The login shell, when one was requested, has to exist before the user that
    # gets it. command -v fails the script if the package did not deliver it.
    if [ -n "${LOGIN_SHELL_PKG:-}" ]; then
      if command -v apk >/dev/null 2>&1; then
        apk add --no-cache "$LOGIN_SHELL_PKG"
      else
        DEBIAN_FRONTEND=noninteractive apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y "$LOGIN_SHELL_PKG"
      fi
      set -- -s "$(command -v "$LOGIN_SHELL_PKG")"
    fi
    if command -v groupadd >/dev/null 2>&1; then
      groupadd --system rig-wheel
      groupadd --system rigtest-user || true
      useradd -d /var/lib/rigtest-user -g rigtest-user -G rig-wheel -p '*' "$@" rigtest-user
      passwd -d rigtest-user || true
    else
      addgroup -S rig-wheel
      addgroup -S rigtest-user || true
      adduser -D -h /var/lib/rigtest-user -G rigtest-user "$@" rigtest-user
      addgroup rigtest-user rig-wheel
      passwd -u rigtest-user || true
    fi
    mkdir -p /var/lib/rigtest-user/
    cp -r /root/.ssh /var/lib/rigtest-user/.
    chown -R rigtest-user:rigtest-user /var/lib/rigtest-user/
    [ ! -d /etc/sudoers.d/ ] || {
      echo '%rig-wheel ALL=(ALL)NOPASSWD:ALL' >/etc/sudoers.d/rig-wheel
      chmod 0440 /etc/sudoers.d/rig-wheel
    }
    [ ! -d /etc/doas.d/ ] || {
      echo 'permit nopass :rig-wheel' >/etc/doas.d/rig-wheel.conf
      chmod 0440 /etc/doas.d/rig-wheel.conf
    }
EOF
  RET=$?
  [ $RET -eq 0 ] || {
    color_echo failed to provision new user rigtest-user >&2
    return 0
  }

  ssh "$@" rigtest-user@127.0.0.1 true || {
    RET=$?
    color_echo failed to SSH into machine as rigtest-user >&2
    return 0
  }

  # Make sure the login shell really is in the way of what rig sends, otherwise this
  # silently degrades into a copy of rig_test_regular_user. The expansion is the one
  # that broke sudo detection in k0sproject/k0sctl#1135: a POSIX shell echoes a shell
  # path, fish refuses to parse it and echoes nothing. Only the output can tell them
  # apart - fish 3.x still exits 0 after the parse error, which is what made the
  # original bug so quiet.
  if [ -n "$shellPkg" ] && [ -n "$(ssh "$@" rigtest-user@127.0.0.1 'echo ${SHELL-sh}' 2>/dev/null)" ]; then
    RET=1
    color_echo "login shell $shellPkg parsed a POSIX parameter expansion, it is not in use" >&2
    return 0
  fi

  # Provisioning only configures sudo or doas when the image ships one. Where it did,
  # rig has to find it, and -expect-sudo makes the suite say so instead of skipping the
  # escalation tests - the failure mode a mangled detection command leads to.
  local -a sudoArgs=()
  if ssh "$@" rigtest-user@127.0.0.1 'sudo -n -- true || doas -n -- true' >/dev/null 2>&1; then
    sudoArgs+=(-expect-sudo)
  fi

  HOME="$(pwd)" go test -v ./ -args -host 127.0.0.1 -port "$sshPort" -user rigtest-user -ssh-keypath .ssh/id_ed25519 "${sudoArgs[@]}"
}

rig_test_openssh_client() {
  color_echo "- Testing openssh client protocol"
  make create-host
  echo "Host testhost" > .ssh/config
  echo "  HostName 127.0.0.1" >> .ssh/config
  echo "  Port $(ssh_port node0)" >> .ssh/config
  echo "  User root" >> .ssh/config
  echo "  IdentityFile $(pwd)/.ssh/id_ed25519" >> .ssh/config
  echo "  UserKnownHostsFile /dev/null" >> .ssh/config
  echo "  StrictHostKeyChecking no" >> .ssh/config
  cat .ssh/config
  set +e
  go test -v ./ -args -ssh-configpath .ssh/config -host testhost -protocol openssh -user ""
  exit_code=$?
  set -e
  RET=$exit_code
}

rig_test_openssh_client_no_multiplex() {
  color_echo "- Testing openssh client protocol without ssh multiplexing"
  make create-host
  echo "Host testhost" > .ssh/config
  echo "  HostName 127.0.0.1" >> .ssh/config
  echo "  Port $(ssh_port node0)" >> .ssh/config
  echo "  User root" >> .ssh/config
  echo "  IdentityFile $(pwd)/.ssh/id_ed25519" >> .ssh/config
  echo "  UserKnownHostsFile /dev/null" >> .ssh/config
  echo "  StrictHostKeyChecking no" >> .ssh/config
  cat .ssh/config
  set +e
  go test -v ./ -args -ssh-configpath .ssh/config -host testhost -protocol openssh -user "" -openssh-multiplex=false
  exit_code=$?
  set -e
  RET=$exit_code
}


retry() {
  local i
  for i in 1 2 3 4 5; do
    ! "$@" || return 0
    sleep $i
  done
  "$@"
}

if [ -z "$FOCUS" ] && ! sanity_check; then
  color_echo Sanity check failed >&2
  exit 1
fi

for test in $(declare -F|grep rig_test_|cut -d" " -f3); do
  if [ "$FOCUS" != "" ] && [ "$FOCUS" != "$test" ]; then
    continue
  fi
  make clean
  color_echo "\n###########################################################"
  RET=0
  $test || RET=$?
  if [ $RET -ne 0 ]; then
    color_echo "Test $test failed" >&2
    exit 1
  fi
  echo -e "\n\n\n"
done
