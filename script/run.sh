#!/usr/bin/with-contenv bashio
# SPDX-License-Identifier: Apache-2.0
# Copyright (C) 2026 SukramJ.
#
# Home Assistant add-on entrypoint for go-fabric-server: maps the add-on
# options (/data/options.json, read with bashio) onto the daemon's
# environment (FABRIC_SERVER_<FLAG>) and exec's the daemon, which becomes
# PID 1 and receives the signals.
set -e

bashio::log.info "Starting go-fabric-server add-on..."

# --- The controller half (/ws) ---
# Loopback only: Home Assistant's integration connects to
# ws://localhost:<port>/ws on the same host and sends no token.
export FABRIC_SERVER_LISTEN="127.0.0.1:$(bashio::config 'port')"
export FABRIC_SERVER_STORAGE_PATH="/data"
export FABRIC_SERVER_LOG_LEVEL="$(bashio::config 'log_level')"
export FABRIC_SERVER_ENABLE_TEST_NET_DCL="$(bashio::config 'enable_test_net_dcl')"

# --- The bridge half (/bridge) and its node ---
export FABRIC_SERVER_BRIDGE="$(bashio::config 'bridge')"
export FABRIC_SERVER_BRIDGE_LISTEN=":$(bashio::config 'bridge_port')"

# The bridge node's surface policies: the lists joined by commas into
# --bridge-extensions and --bridge-profile, the level into
# --bridge-spec-level; the daemon refuses an unknown value with the known
# ones.
join_list() {
    local out="" item
    while IFS= read -r item; do
        [ -n "${item}" ] || continue
        out="${out:+${out},}${item}"
    done < <(bashio::config "$1")
    printf '%s' "${out}"
}
if bashio::config.has_value 'surface_extensions'; then
    export FABRIC_SERVER_BRIDGE_EXTENSIONS="$(join_list 'surface_extensions')"
    bashio::log.info "Bridge node extensions: ${FABRIC_SERVER_BRIDGE_EXTENSIONS}"
fi
if bashio::config.has_value 'surface_level'; then
    export FABRIC_SERVER_BRIDGE_SPEC_LEVEL="$(bashio::config 'surface_level')"
fi
if bashio::config.has_value 'surface_profiles'; then
    export FABRIC_SERVER_BRIDGE_PROFILE="$(join_list 'surface_profiles')"
fi

# --- The node pages through Ingress ---
# On the host's network: listen on ingress_port and answer only the Ingress
# proxy, which is also the pages' administrator.
export FABRIC_SERVER_WEB="0.0.0.0:8099"
export FABRIC_SERVER_WEB_ADMIN_SOURCE="172.30.32.2"

# --- BLE commissioning through ESPHome Bluetooth proxies ---
# One --bluetooth-proxy per entry (host[:port][,key]); the keys stay out of
# the log.
args=()
if bashio::config.has_value 'bluetooth_proxies'; then
    while IFS= read -r proxy; do
        [ -n "${proxy}" ] || continue
        args+=(--bluetooth-proxy "${proxy}")
        bashio::log.info "Bluetooth proxy ${proxy%%,*}"
    done < <(bashio::config 'bluetooth_proxies')
fi

bashio::log.info "API on ws://${FABRIC_SERVER_LISTEN}/ws, store /data; node pages through Ingress."

exec /usr/bin/fabric-server "${args[@]}"
