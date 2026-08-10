# Contributing

Thanks for helping improve `nimiq-go`.

## Setup

- Go **1.25+**
- Optional: [golangci-lint](https://golangci-lint.run/) for local linting

```bash
git clone https://github.com/NimMiniApps/nimiq-go.git
cd nimiq-go
make check
```

## Workflow

1. Open an issue for non-trivial changes when useful.
2. Branch from `main`.
3. Keep PRs focused; match existing package style and godoc tone.
4. Run `make check` before opening the PR.
5. Do not commit private keys, wallet exports, or real mainnet seeds. Tests use fixed synthetic seeds only.

## Packages

- Root `nimiq` — protocol types and offline crypto
- `signer` — private-key `Signer` implementations (keep key material out of `rpc`)
- `rpc` — JSON-RPC only; never signs

## Releases

1. Update `CHANGELOG.md` with a new version section.
2. Merge to `main`.
3. Tag `vX.Y.Z` on the merge commit and push the tag.
4. The `release` workflow creates the GitHub Release from the CHANGELOG section.

Module consumers install with:

```bash
go get github.com/NimMiniApps/nimiq-go@vX.Y.Z
```
