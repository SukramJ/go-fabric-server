# go-fabric-server — Home Assistant add-on

This add-on runs [go-fabric-server](https://github.com/SukramJ/go-fabric-server)
inside Home Assistant: a Matter controller server on go-fabric that Home
Assistant's Matter integration uses in place of the official Matter Server
(wire-compatible with matterjs-server, schema 14). Uncertified.

## Installation

1. In Home Assistant go to **Settings → Add-ons → Add-on Store**.
2. Open the **⋮** menu → **Repositories** and add
   `https://github.com/SukramJ/go-fabric-server`.
3. Stop the official **Matter Server** add-on if it runs, install and start
   **go-fabric-server**, then add the Matter integration with the URL
   `ws://localhost:5580/ws` ([`DOCS.md`](DOCS.md)).

## Images

The add-on pulls `ghcr.io/sukramj/go-fabric-server-addon-{arch}`
(`.github/workflows/addon-image.yml`): a Home Assistant base image with the
daemon and `script/run.sh`. `addon/Dockerfile` builds the same image
locally when the manifest's `image:` key is removed.
