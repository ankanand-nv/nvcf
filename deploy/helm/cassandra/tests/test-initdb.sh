#!/bin/sh
# SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -eu

test_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
chart_dir=$(CDPATH='' cd -- "${test_dir}/.." && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "${work_dir}"' EXIT

mkdir -p "${work_dir}/bin"

cat > "${work_dir}/bin/kubectl" <<'EOF'
#!/bin/sh
case "$*" in
  *"get statefulset"*)
    printf '1'
    ;;
  *"get pod"*)
    printf 'true'
    ;;
  *"nodetool statusbinary"*)
    attempts=0
    if [ -f "${TEST_STATE}" ]; then
      attempts=$(cat "${TEST_STATE}")
    fi
    attempts=$((attempts + 1))
    printf '%s\n' "${attempts}" > "${TEST_STATE}"
    if [ "${attempts}" -eq 1 ]; then
      printf 'not running\n'
    else
      printf 'running\n'
    fi
    ;;
  *"cqlsh"*)
    cat >/dev/null
    attempts=0
    if [ -f "${TEST_AUTH_STATE}" ]; then
      attempts=$(cat "${TEST_AUTH_STATE}")
    fi
    attempts=$((attempts + 1))
    printf '%s\n' "${attempts}" > "${TEST_AUTH_STATE}"
    if [ "${attempts}" -le 2 ]; then
      exit 1
    fi
    ;;
  *)
    printf 'unexpected kubectl command: %s\n' "$*" >&2
    exit 1
    ;;
esac
EOF

cat > "${work_dir}/bin/sleep" <<'EOF'
#!/bin/sh
exit 0
EOF

chmod +x "${work_dir}/bin/kubectl" "${work_dir}/bin/sleep"

export TEST_STATE="${work_dir}/native-transport-attempts"
export TEST_AUTH_STATE="${work_dir}/authentication-attempts"
output=$(
  PATH="${work_dir}/bin:${PATH}" \
    CASSANDRA_USER=cassandra \
    CASSANDRA_PASSWORD=desired-password \
    bash "${chart_dir}/helm/scripts/initdb.sh"
)

printf '%s\n' "${output}" | grep -Fq \
  'Waiting for Cassandra native transport on pod cassandra-0...'
printf '%s\n' "${output}" | grep -Fq \
  'Waiting for Cassandra superuser authentication on pod cassandra-0...'
printf '%s\n' "${output}" | grep -Fq 'Successfully initialized the db'
[ "$(cat "${TEST_STATE}")" -eq 2 ]
[ "$(cat "${TEST_AUTH_STATE}")" -eq 5 ]
