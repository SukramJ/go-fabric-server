# Compatibility fixtures

The JSON examples of matterjs-server's `docs/websockets_api.md`
(<https://github.com/matter-js/matterjs-server>, Apache-2.0, commit
`a7922e85f4b94a5db94a1dbee1a02c685c2ec71d`, version 1.5.5), one file per
fenced `json` block that parses, named `<command or event>.<kind>.json`
after the heading above it; `kind` is `request`, `response`, `event` or
`value` (a bare result). One example is adapted: `node_added.event.json`
has `"attributes": {}` where the documentation writes `{ ... }`.

`script/extract-compat-fixtures.py` regenerates them from a checkout:

```sh
python3 -I script/extract-compat-fixtures.py <matterjs-server>/docs/websockets_api.md protocol/testdata/compat
```

`controllerapi/compat_internal_test.go` holds the server's Go types against
them: every request decodes through the protocol's decoder, and every
result and event of the S1 set round-trips through the type the server
encodes it with, equal modulo key order (and modulo the documented
extensions of go-fabric-server, such as `capabilities` in `server_info`).

`python/` holds the S3 shapes matterjs-server's documentation has no example
of, recorded in the shape of its python client (`python/README.md`); they
are checked the same way.
