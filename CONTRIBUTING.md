# Contributing

Thanks for your interest in Murphy.

## Before you start

- For anything bigger than a small fix, open an issue first, so we can agree on the approach before you write code.
- Read the [architecture overview](docs/architecture.md) and the [development guide](docs/development.md). Every change follows them.

## Contributor License Agreement

Murphy is licensed under [AGPL-3.0](LICENSE). Outside contributions can only be merged once you have signed a Contributor License Agreement, which lets the project keep its licensing options open. A maintainer will send it to you on your PR.

## Pull requests

- `main` is protected: every change goes through a pull request.
- The PR title is a [Conventional Commit](https://www.conventionalcommits.org): `type(scope): subject`, with a lower-case subject and no trailing period. PRs are squash-merged, so the title becomes the commit message and the description its body.
- Commits must be signed.
- A change to the architecture updates `docs/architecture.md` in the same PR.
- CI must pass: formatting, vet, lint and tests.
