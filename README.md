# go-fabric-server

A stand-alone daemon on [go-fabric](https://github.com/SukramJ/go-fabric) with a WebSocket/JSON API in both Matter roles: a controller server wire-compatible with the API Home Assistant's Matter integration speaks (the python-matter-server / matterjs-server contract), and a bridge door for hosts that are not written in Go.

> **Status: concept accepted 2026-10-24, implementation starting.** The design is go-fabric's concept v8; this repository's `CONCEPT.md`, protocol and code arrive with the first bundle (S1).

"Matter" and the Matter logo are trademarks of the Connectivity Standards Alliance; the word appears here only descriptively. See go-fabric's README, section "Not certified".

Apache-2.0, see [LICENSE](LICENSE).
